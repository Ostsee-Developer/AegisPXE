package agentcontrol

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/agent"
	"github.com/Ostsee-Developer/AegisPXE/internal/agenttrust"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
)

const testAgentID = "550e8400-e29b-41d4-a716-446655440000"

type fakeBackend struct {
	record               agent.Record
	certificate          agent.Certificate
	completedSecret      string
	completedCertificate agent.Certificate
	heartbeat            agent.Heartbeat
	heartbeatFingerprint string
}

func (f *fakeBackend) ManagedAgent(_ context.Context, id string) (agent.Record, error) {
	if id != f.record.ID {
		return agent.Record{}, fault.New(fault.AgentNotFound, "not found", nil)
	}
	return f.record, nil
}

func (f *fakeBackend) CompleteAgentEnrollment(_ context.Context, id, secret string, certificate agent.Certificate, _ string) (agent.Record, error) {
	if id != f.record.ID || secret != "bootstrap-secret-value-12345678901234567890" {
		return agent.Record{}, fault.New(fault.AgentEnrollmentInvalid, "invalid", nil)
	}
	f.completedSecret = secret
	f.completedCertificate = certificate
	f.record.State = agent.StateOffline
	return f.record, nil
}

func (f *fakeBackend) AuthenticateAgentCertificate(_ context.Context, fingerprint string) (agent.Record, agent.Certificate, error) {
	if fingerprint != f.certificate.Fingerprint {
		return agent.Record{}, agent.Certificate{}, fault.New(fault.AgentCertificateInvalid, "invalid", nil)
	}
	return f.record, f.certificate, nil
}

func (f *fakeBackend) RecordAgentHeartbeat(_ context.Context, id, fingerprint string, heartbeat agent.Heartbeat, _ string) (agent.Record, error) {
	if id != f.record.ID || fingerprint != f.certificate.Fingerprint {
		return agent.Record{}, fault.New(fault.AgentCertificateInvalid, "invalid", nil)
	}
	f.heartbeat = heartbeat
	f.heartbeatFingerprint = fingerprint
	f.record.State = agent.StateOnline
	f.record.ActiveGeneration = heartbeat.Generation
	f.record.ActiveVersion = heartbeat.Version
	return f.record, nil
}

func TestEnrollmentIssuesAgentCertificateWithoutEchoingCredential(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	authority, err := agenttrust.LoadOrCreate(t.TempDir(), logger)
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeBackend{record: agent.Record{ID: testAgentID, InstallationID: "i_test", MachineID: "m_test", State: agent.StateUnenrolled, DesiredGeneration: 1}}
	server, err := New(backend, authority, logger)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server.now = func() time.Time { return now }
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := enrollmentRequest{
		AgentID:        testAgentID,
		InstallationID: "i_test",
		MachineID:      "m_test",
		Credential:     "bootstrap-secret-value-12345678901234567890",
		PublicKey:      base64.RawURLEncoding.EncodeToString(publicKey),
	}
	body, _ := json.Marshal(payload)
	request := httptest.NewRequest(http.MethodPost, "https://agent.test/v1/enroll", bytes.NewReader(body))
	request.TLS = &tls.ConnectionState{}
	request.Header.Set("X-Request-ID", "req_enroll_test")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if backend.completedSecret != payload.Credential || backend.completedCertificate.AgentID != testAgentID {
		t.Fatalf("enrollment was not persisted: %+v", backend.completedCertificate)
	}
	if bytes.Contains(response.Body.Bytes(), []byte(payload.Credential)) {
		t.Fatal("enrollment response echoed bootstrap credential")
	}
	var output enrollmentResponse
	if err := json.Unmarshal(response.Body.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode([]byte(output.CertificatePEM))
	if block == nil {
		t.Fatal("enrollment response did not contain a certificate")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if certificate.Subject.CommonName != "aegispxe-agent:"+testAgentID || output.Fingerprint == "" {
		t.Fatalf("unexpected issued certificate: cn=%q fingerprint=%q", certificate.Subject.CommonName, output.Fingerprint)
	}
}

func TestHeartbeatRequiresVerifiedMTLSCertificate(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	authority, err := agenttrust.LoadOrCreate(t.TempDir(), logger)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	issued, err := authority.IssueClientCertificate(testAgentID, publicKey, now)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(issued.PEM)
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeBackend{
		record:      agent.Record{ID: testAgentID, InstallationID: "i_test", MachineID: "m_test", State: agent.StateOffline, DesiredGeneration: 2},
		certificate: agent.Certificate{Fingerprint: issued.Fingerprint, AgentID: testAgentID, Serial: issued.Serial, IssuedAt: now, ExpiresAt: issued.ExpiresAt},
	}
	server, err := New(backend, authority, logger)
	if err != nil {
		t.Fatal(err)
	}
	heartbeat := agent.Heartbeat{Version: "0.2.0-dev.1", Generation: 1, BootID: "boot-1", UptimeSeconds: 50, Hostname: "node", Kernel: "6.12", Architecture: "amd64"}
	body, _ := json.Marshal(heartbeat)

	missing := httptest.NewRequest(http.MethodPost, "https://agent.test/v1/heartbeat", bytes.NewReader(body))
	missing.TLS = &tls.ConnectionState{}
	missingResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(missingResponse, missing)
	if missingResponse.Code != http.StatusUnauthorized {
		t.Fatalf("heartbeat without mTLS status=%d body=%s", missingResponse.Code, missingResponse.Body.String())
	}

	request := httptest.NewRequest(http.MethodPost, "https://agent.test/v1/heartbeat", bytes.NewReader(body))
	request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{certificate}, VerifiedChains: [][]*x509.Certificate{{certificate}}}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", response.Code, response.Body.String())
	}
	if backend.heartbeat.BootID != heartbeat.BootID || backend.heartbeatFingerprint != issued.Fingerprint {
		t.Fatalf("heartbeat not authenticated/persisted: %+v", backend.heartbeat)
	}
	var output heartbeatResponse
	if err := json.Unmarshal(response.Body.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if !output.UpdateAvailable || output.DesiredGeneration != 2 {
		t.Fatalf("unexpected heartbeat response: %+v", output)
	}
}

func TestTLSConfigUsesTLS13AndOptionalVerifiedClientCertificate(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	authority, err := agenttrust.LoadOrCreate(t.TempDir(), logger)
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeBackend{record: agent.Record{ID: testAgentID}}
	server, err := New(backend, authority, logger)
	if err != nil {
		t.Fatal(err)
	}
	config, err := server.TLSConfig("https://127.0.0.1:8092")
	if err != nil {
		t.Fatal(err)
	}
	if config.MinVersion != tls.VersionTLS13 || config.ClientAuth != tls.VerifyClientCertIfGiven || len(config.Certificates) != 1 || config.ClientCAs == nil {
		t.Fatalf("unexpected agent control TLS config: %+v", config)
	}
}
