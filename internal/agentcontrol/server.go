package agentcontrol

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/agent"
	"github.com/Ostsee-Developer/AegisPXE/internal/agenttrust"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
)

const maxAgentControlBody = 16 * 1024

type Backend interface {
	ManagedAgent(context.Context, string) (agent.Record, error)
	CompleteAgentEnrollment(context.Context, string, string, agent.Certificate, string) (agent.Record, error)
	AuthenticateAgentCertificate(context.Context, string) (agent.Record, agent.Certificate, error)
	RecordAgentHeartbeat(context.Context, string, string, agent.Heartbeat, string) (agent.Record, error)
}

type Server struct {
	state     Backend
	authority *agenttrust.Authority
	logger    *slog.Logger
	now       func() time.Time
}

type enrollmentRequest struct {
	AgentID        string `json:"agent_id"`
	InstallationID string `json:"installation_id"`
	MachineID      string `json:"machine_id"`
	Credential     string `json:"credential"`
	PublicKey      string `json:"public_key"`
}

type enrollmentResponse struct {
	CertificatePEM string    `json:"certificate_pem"`
	Fingerprint    string    `json:"fingerprint"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type heartbeatResponse struct {
	Status            string `json:"status"`
	DesiredGeneration int    `json:"desired_generation"`
	UpdateAvailable   bool   `json:"update_available"`
}

func New(state Backend, authority *agenttrust.Authority, logger *slog.Logger) (*Server, error) {
	if state == nil || authority == nil {
		return nil, errors.New("agent control plane requires state and trust authority")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{state: state, authority: authority, logger: logger, now: time.Now}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/enroll", s.handleEnroll)
	mux.HandleFunc("POST /v1/heartbeat", s.handleHeartbeat)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return mux
}

func (s *Server) TLSConfig(controllerURL string) (*tls.Config, error) {
	certificate, err := s.authority.NewServerCertificate(controllerURL, s.now())
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		ClientCAs:    s.authority.ClientCAPool(),
		ClientAuth:   tls.VerifyClientCertIfGiven,
	}, nil
}

func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	requestID := requestID(r)
	if r.TLS == nil {
		s.reject(w, r, requestID, "", fault.AgentEnrollmentInvalid, "TLS is required for managed agent enrollment", http.StatusBadRequest)
		return
	}
	var input enrollmentRequest
	if err := decodeBoundedJSON(w, r, &input); err != nil {
		s.reject(w, r, requestID, "", fault.AgentEnrollmentInvalid, "managed agent enrollment request is invalid", http.StatusBadRequest)
		return
	}
	input.AgentID = strings.TrimSpace(input.AgentID)
	input.InstallationID = strings.TrimSpace(input.InstallationID)
	input.MachineID = strings.TrimSpace(input.MachineID)
	input.Credential = strings.TrimSpace(input.Credential)
	input.PublicKey = strings.TrimSpace(input.PublicKey)
	record, err := s.state.ManagedAgent(r.Context(), input.AgentID)
	if err != nil {
		s.writeFault(w, r, requestID, input.AgentID, err)
		return
	}
	if input.InstallationID == "" || input.MachineID == "" || record.InstallationID != input.InstallationID || record.MachineID != input.MachineID {
		s.reject(w, r, requestID, input.AgentID, fault.AgentEnrollmentInvalid, "managed agent enrollment binding is invalid", http.StatusUnauthorized)
		return
	}
	publicKeyBytes, err := base64.RawURLEncoding.DecodeString(input.PublicKey)
	if err != nil || len(publicKeyBytes) != ed25519.PublicKeySize {
		s.reject(w, r, requestID, input.AgentID, fault.AgentEnrollmentInvalid, "managed agent enrollment public key is invalid", http.StatusBadRequest)
		return
	}
	issuedAt := s.now().UTC()
	issued, err := s.authority.IssueClientCertificate(input.AgentID, ed25519.PublicKey(publicKeyBytes), issuedAt)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "managed agent certificate issuance failed", "component", "agent.control", "operation", "enroll", "request_id", requestID, "agent_id", input.AgentID, "installation_id", record.InstallationID, "machine_id", record.MachineID, "error_code", fault.AgentTrustUnavailable, "result", "failure", "error", err)
		writeError(w, http.StatusInternalServerError, fault.AgentTrustUnavailable, "managed agent trust authority is unavailable")
		return
	}
	publicKeyDigest := sha256.Sum256(publicKeyBytes)
	certificate := agent.Certificate{
		Fingerprint:     issued.Fingerprint,
		AgentID:         input.AgentID,
		Serial:          issued.Serial,
		PublicKeySHA256: hex.EncodeToString(publicKeyDigest[:]),
		IssuedAt:        issuedAt,
		ExpiresAt:       issued.ExpiresAt,
	}
	if _, err := s.state.CompleteAgentEnrollment(r.Context(), input.AgentID, input.Credential, certificate, requestID); err != nil {
		s.writeFault(w, r, requestID, input.AgentID, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, enrollmentResponse{CertificatePEM: string(issued.PEM), Fingerprint: issued.Fingerprint, ExpiresAt: issued.ExpiresAt})
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	requestID := requestID(r)
	record, fingerprint, err := s.authenticateMTLS(r)
	if err != nil {
		s.writeFault(w, r, requestID, "", err)
		return
	}
	var heartbeat agent.Heartbeat
	if err := decodeBoundedJSON(w, r, &heartbeat); err != nil {
		s.reject(w, r, requestID, record.ID, fault.AgentHeartbeatInvalid, "managed agent heartbeat request is invalid", http.StatusBadRequest)
		return
	}
	updated, err := s.state.RecordAgentHeartbeat(r.Context(), record.ID, fingerprint, heartbeat, requestID)
	if err != nil {
		s.writeFault(w, r, requestID, record.ID, err)
		return
	}
	writeJSON(w, http.StatusOK, heartbeatResponse{Status: "ok", DesiredGeneration: updated.DesiredGeneration, UpdateAvailable: updated.DesiredGeneration > updated.ActiveGeneration})
}

func (s *Server) authenticateMTLS(r *http.Request) (agent.Record, string, error) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) != 1 || len(r.TLS.VerifiedChains) == 0 {
		return agent.Record{}, "", fault.New(fault.AgentCertificateInvalid, "managed agent client certificate is required", nil)
	}
	certificate := r.TLS.PeerCertificates[0]
	const prefix = "aegispxe-agent:"
	if !strings.HasPrefix(certificate.Subject.CommonName, prefix) {
		return agent.Record{}, "", fault.New(fault.AgentCertificateInvalid, "managed agent client certificate subject is invalid", nil)
	}
	agentID := strings.TrimPrefix(certificate.Subject.CommonName, prefix)
	if err := agent.ValidateID(agentID); err != nil {
		return agent.Record{}, "", fault.New(fault.AgentCertificateInvalid, "managed agent client certificate identity is invalid", err)
	}
	digest := sha256.Sum256(certificate.Raw)
	fingerprint := hex.EncodeToString(digest[:])
	record, _, err := s.state.AuthenticateAgentCertificate(r.Context(), fingerprint)
	if err != nil {
		return agent.Record{}, "", err
	}
	if record.ID != agentID {
		return agent.Record{}, "", fault.New(fault.AgentCertificateInvalid, "managed agent client certificate binding mismatch", nil)
	}
	return record, fingerprint, nil
}

func (s *Server) writeFault(w http.ResponseWriter, r *http.Request, requestID, agentID string, err error) {
	code := fault.Code(err)
	status := http.StatusInternalServerError
	switch code {
	case fault.AgentNotFound:
		status = http.StatusNotFound
	case fault.AgentEnrollmentInvalid, fault.AgentCertificateInvalid:
		status = http.StatusUnauthorized
	case fault.AgentEnrollmentExpired, fault.AgentEnrollmentReplay, fault.AgentConflict:
		status = http.StatusConflict
	case fault.AgentCertificateRevoked:
		status = http.StatusForbidden
	case fault.AgentHeartbeatInvalid, fault.AgentInvalid:
		status = http.StatusBadRequest
	}
	if code == "" {
		code = fault.StorageFailure
	}
	s.logger.WarnContext(r.Context(), "managed agent control request rejected", "component", "agent.control", "operation", r.URL.Path, "request_id", requestID, "agent_id", agentID, "error_code", code, "result", "rejected")
	writeError(w, status, code, "managed agent request rejected")
}

func (s *Server) reject(w http.ResponseWriter, r *http.Request, requestID, agentID, code, message string, status int) {
	s.logger.WarnContext(r.Context(), "managed agent control request rejected", "component", "agent.control", "operation", r.URL.Path, "request_id", requestID, "agent_id", agentID, "error_code", code, "result", "rejected")
	writeError(w, status, code, message)
}

func decodeBoundedJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxAgentControlBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request contains trailing JSON")
	}
	return nil
}

func requestID(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("X-Request-ID"))
	if value != "" && len(value) <= 128 && !strings.ContainsAny(value, "\r\n\x00") {
		return value
	}
	generated, err := idgen.New("req_")
	if err != nil {
		return "req_unavailable"
	}
	return generated
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error_code": code, "message": message})
}
