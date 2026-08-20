package agenttrust

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadOrCreatePersistsStableAuthority(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "trust")
	first, err := LoadOrCreate(directory, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreate(directory, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.InstanceID() != second.InstanceID() || first.UpdateVerifyKeyB64() != second.UpdateVerifyKeyB64() || first.CAPEM() != second.CAPEM() {
		t.Fatal("agent trust authority changed after reload")
	}
	for _, name := range []string{caKeyFile, updateKeyFile} {
		info, err := os.Stat(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode=%o want=600", name, info.Mode().Perm())
		}
	}
}

func TestAuthoritySignsAndIssuesCertificates(t *testing.T) {
	authority, err := LoadOrCreate(filepath.Join(t.TempDir(), "trust"), nil)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"agent_id":"test"}`)
	signatureB64, err := authority.SignUpdateManifest(payload)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := base64.RawURLEncoding.DecodeString(signatureB64)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(authority.UpdateVerifyKeyB64())
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), payload, signature) {
		t.Fatal("update manifest signature did not verify")
	}

	clientPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := authority.IssueClientCertificate("550e8400-e29b-41d4-a716-446655440000", clientPublic, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(issued.PEM) == 0 || issued.Serial == "" || issued.Fingerprint == "" || issued.ExpiresAt.IsZero() {
		t.Fatalf("incomplete issued certificate: %+v", issued)
	}
	if _, err := authority.NewServerCertificate("https://192.0.2.10:8092", time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestLoadOrCreateRejectsIncompleteOrBroadPrivateMaterial(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "trust")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, caKeyFile), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(directory, nil); err == nil {
		t.Fatal("incomplete trust material was accepted")
	}
}
