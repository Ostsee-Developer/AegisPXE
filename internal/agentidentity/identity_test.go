package agentidentity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"reflect"
	"testing"
	"time"
)

func TestSealAndReadRoundTrip(t *testing.T) {
	identity := testIdentity(t)
	sealed, err := Seal([]byte("ELF-TEMPLATE"), identity)
	if err != nil {
		t.Fatal(err)
	}
	if !HasIdentity(sealed) {
		t.Fatal("sealed binary did not contain identity trailer")
	}
	got, err := Read(sealed)
	if err != nil {
		t.Fatal(err)
	}
	identity, err = identity.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, identity) {
		t.Fatalf("identity=%+v want=%+v", got, identity)
	}
}

func TestReadRejectsTamperedIdentity(t *testing.T) {
	sealed, err := Seal([]byte("ELF-TEMPLATE"), testIdentity(t))
	if err != nil {
		t.Fatal(err)
	}
	sealed[len("ELF-TEMPLATE")+3] ^= 0xff
	if _, err := Read(sealed); err == nil {
		t.Fatal("tampered identity was accepted")
	}
}

func TestSealRejectsAlreadySealedTemplate(t *testing.T) {
	sealed, err := Seal([]byte("ELF-TEMPLATE"), testIdentity(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Seal(sealed, testIdentity(t)); err == nil {
		t.Fatal("already sealed template was accepted")
	}
}

func testIdentity(t *testing.T) Identity {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "AegisPXE Agent Test CA"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return Identity{
		SchemaVersion:      SchemaVersion,
		AgentID:            "550e8400-e29b-41d4-a716-446655440000",
		InstallationID:     "i_test",
		MachineID:          "m_test",
		InstanceID:         "aegispxe_test_01",
		ControllerURL:      "https://192.0.2.10:8092",
		Version:            "0.2.0-dev.1",
		Generation:         1,
		Architecture:       "amd64",
		CapabilityCeiling:  []string{"service.status", "diagnostics.read"},
		ServerCAPEM:        string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		UpdateVerifyKeyB64: base64.RawURLEncoding.EncodeToString(publicKey),
	}
}
