package agentbuild

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/agent"
	"github.com/Ostsee-Developer/AegisPXE/internal/agentidentity"
)

type testAuthority struct {
	instanceID string
	caPEM      string
	privateKey ed25519.PrivateKey
}

func (a testAuthority) InstanceID() string { return a.instanceID }
func (a testAuthority) CAPEM() string      { return a.caPEM }
func (a testAuthority) UpdateVerifyKeyB64() string {
	return base64.RawURLEncoding.EncodeToString(a.privateKey.Public().(ed25519.PublicKey))
}
func (a testAuthority) SignUpdateManifest(payload []byte) (string, error) {
	return base64.RawURLEncoding.EncodeToString(ed25519.Sign(a.privateKey, payload)), nil
}

func TestBuilderCreatesSignedPerInstallationDebWithSealedIdentity(t *testing.T) {
	authority := newTestAuthority(t)
	templatePath := filepath.Join(t.TempDir(), "agent-template")
	if err := os.WriteFile(templatePath, []byte("ELF-TEMPLATE-DATA"), 0o755); err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(t.TempDir(), "builds")
	if err := os.Mkdir(outputDir, 0o700); err != nil {
		t.Fatal(err)
	}
	builder, err := New(Config{TemplatePath: templatePath, OutputDir: outputDir, ControllerURL: "https://pxe.example.test:8092", Version: "0.2.0-dev.1"}, authority, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	record := agent.Record{ID: "550e8400-e29b-41d4-a716-446655440000", InstallationID: "i_test", MachineID: "m_test", State: agent.StatePendingBuild, UpdateMode: agent.UpdateModeManual, UpdateState: agent.UpdateStateIdle, CapabilityCeiling: []string{"diagnostics.read", "service.status"}, DesiredGeneration: 1, LastHeartbeatJSON: "{}", CreatedAt: now, UpdatedAt: now}
	build := agent.Build{ID: "ab_test", AgentID: record.ID, Generation: 1, Architecture: "amd64", CapabilityCeiling: append([]string(nil), record.CapabilityCeiling...), State: agent.BuildStateBuilding, CreatedAt: now, StartedAt: now}
	artifact, err := builder.Build(context.Background(), record, build)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.PackageSize <= 0 || len(artifact.PackageSHA256) != 64 || len(artifact.ManifestSHA256) != 64 || artifact.ManifestSignature == "" {
		t.Fatalf("unexpected artifact metadata: %+v", artifact)
	}
	packagePath := filepath.Join(outputDir, artifact.PackagePath)
	packageBytes, err := os.ReadFile(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	if Digest(packageBytes) != artifact.PackageSHA256 {
		t.Fatal("persisted package digest mismatch")
	}
	binary := extractDebFile(t, packageBytes, "usr/bin/aegispxe-agent")
	identity, err := agentidentity.Read(binary)
	if err != nil {
		t.Fatal(err)
	}
	if identity.AgentID != record.ID || identity.InstallationID != record.InstallationID || identity.MachineID != record.MachineID || identity.Generation != 1 || identity.InstanceID != authority.InstanceID() {
		t.Fatalf("unexpected sealed identity: %+v", identity)
	}
	manifestPath := strings.TrimSuffix(packagePath, ".deb") + ".manifest.json"
	manifestJSON, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if Digest(manifestJSON) != artifact.ManifestSHA256 {
		t.Fatal("manifest digest mismatch")
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	signature, err := base64.RawURLEncoding.DecodeString(artifact.ManifestSignature)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(authority.privateKey.Public().(ed25519.PublicKey), manifestJSON, signature) {
		t.Fatal("manifest signature did not verify")
	}
}

func TestBuilderRejectsAlreadySealedTemplate(t *testing.T) {
	authority := newTestAuthority(t)
	template, err := agentidentity.Seal([]byte("template"), agentidentity.Identity{SchemaVersion: agentidentity.SchemaVersion, AgentID: "550e8400-e29b-41d4-a716-446655440000", InstallationID: "i_test", MachineID: "m_test", InstanceID: authority.InstanceID(), ControllerURL: "https://pxe.example.test:8092", Version: "0.2.0-dev.1", Generation: 1, Architecture: "amd64", ServerCAPEM: authority.CAPEM(), UpdateVerifyKeyB64: authority.UpdateVerifyKeyB64()})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "template")
	if err := os.WriteFile(path, template, 0o755); err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(t.TempDir(), "out")
	if err := os.Mkdir(outputDir, 0o700); err != nil {
		t.Fatal(err)
	}
	builder, err := New(Config{TemplatePath: path, OutputDir: outputDir, ControllerURL: "https://pxe.example.test:8092", Version: "0.2.0-dev.1"}, authority, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	record := agent.Record{ID: "550e8400-e29b-41d4-a716-446655440000", InstallationID: "i_test", MachineID: "m_test", State: agent.StatePendingBuild, UpdateMode: agent.UpdateModeManual, UpdateState: agent.UpdateStateIdle, DesiredGeneration: 1, LastHeartbeatJSON: "{}", CreatedAt: now, UpdatedAt: now}
	_, err = builder.Build(context.Background(), record, agent.Build{ID: "ab", AgentID: record.ID, Generation: 1, Architecture: "amd64", State: agent.BuildStateBuilding, CreatedAt: now})
	if err == nil || !strings.Contains(err.Error(), "must not already contain an identity") {
		t.Fatalf("sealed template err=%v", err)
	}
}

func TestBuilderRejectsNonHTTPSController(t *testing.T) {
	authority := newTestAuthority(t)
	_, err := New(Config{TemplatePath: "/tmp/template", OutputDir: "/tmp/out", ControllerURL: "http://pxe.example.test:8092", Version: "0.2.0-dev.1"}, authority, nil)
	if err == nil {
		t.Fatal("non-HTTPS controller was accepted")
	}
}

func newTestAuthority(t *testing.T) testAuthority {
	t.Helper()
	caPublic, caPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	now := time.Now().UTC()
	certificate := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "test agent CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, caPublic, caPrivate)
	if err != nil {
		t.Fatal(err)
	}
	_, updatePrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return testAuthority{instanceID: "aegispxe_testinstance", caPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), privateKey: updatePrivate}
}

func extractDebFile(t *testing.T, deb []byte, wanted string) []byte {
	t.Helper()
	members, err := readArMembers(deb)
	if err != nil {
		t.Fatal(err)
	}
	data, ok := members["data.tar.gz"]
	if !ok {
		t.Fatal("data.tar.gz missing")
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name != wanted {
			continue
		}
		payload, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		return payload
	}
	t.Fatalf("%s missing from data archive", wanted)
	return nil
}

func readArMembers(data []byte) (map[string][]byte, error) {
	if len(data) < 8 || string(data[:8]) != "!<arch>\n" {
		return nil, errors.New("invalid ar signature")
	}
	members := map[string][]byte{}
	position := 8
	for position < len(data) {
		if position+60 > len(data) {
			return nil, errors.New("truncated ar header")
		}
		header := data[position : position+60]
		position += 60
		name := strings.TrimSuffix(strings.TrimSpace(string(header[:16])), "/")
		var size int
		if _, err := fmt.Sscanf(strings.TrimSpace(string(header[48:58])), "%d", &size); err != nil {
			return nil, err
		}
		if size < 0 || position+size > len(data) {
			return nil, errors.New("invalid ar member size")
		}
		members[name] = append([]byte(nil), data[position:position+size]...)
		position += size
		if size%2 != 0 {
			position++
		}
	}
	return members, nil
}
