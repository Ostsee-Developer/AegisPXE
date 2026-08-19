package store

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/assignment"
	"github.com/Ostsee-Developer/AegisPXE/internal/boottrust"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
)

func TestBootTrustApprovalProofAndConsumedAssignmentRelease(t *testing.T) {
	state := testStore(t)
	ctx := context.Background()
	spec := telemetryInstallation(t, state, "BC:24:11:00:30:01")
	if _, err := state.SetMachinePolicy(ctx, spec.MachineID, machine.PolicyProvision, "req_policy", "test:operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := state.ArmInstallation(ctx, spec.MachineID, spec.ID, "req_arm", "test:operator"); err != nil {
		t.Fatal(err)
	}

	privateKey, publicPEM := testBootTrustRSAKey(t)
	enrolled, created, err := state.RegisterBootTrustKey(ctx, spec.ID, publicPEM, "", "req_enroll")
	if err != nil || !created || enrolled.State != boottrust.KeyPending {
		t.Fatalf("enrollment=%+v created=%v err=%v", enrolled, created, err)
	}
	approved, err := state.ApproveBootTrustKey(ctx, spec.MachineID, enrolled.Fingerprint, "req_approve", "test:admin")
	if err != nil || approved.State != boottrust.KeyApproved {
		t.Fatalf("approval=%+v err=%v", approved, err)
	}

	consumed, err := state.ConsumeAssignment(ctx, spec.ID, "req_consume", "system:pxe")
	if err != nil || consumed.State != assignment.StateConsumed {
		t.Fatalf("consume=%+v err=%v", consumed, err)
	}
	challenge, err := state.CreateBootTrustChallenge(ctx, spec.ID, approved.Fingerprint, "req_challenge")
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := boottrust.CanonicalChallenge(challenge)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(canonical)
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	release, err := state.CompleteBootTrustChallenge(ctx, spec.ID, challenge.ID, signature, "req_prove")
	if err != nil || release.Duplicate || len(release.Ciphertext) == 0 {
		t.Fatalf("release=%+v err=%v", release, err)
	}
	secret, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, privateKey, release.Ciphertext, []byte(lifecycleCredentialOAEPLabel+"\x00"))
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) < 32 {
		t.Fatalf("decrypted credential unexpectedly short: %d", len(secret))
	}
	if _, err := state.AuthenticateLifecycleCredential(ctx, spec.ID, string(secret)); err != nil {
		t.Fatalf("released credential did not authenticate: %v", err)
	}

	duplicate, err := state.CompleteBootTrustChallenge(ctx, spec.ID, challenge.ID, signature, "req_prove_retry")
	if err != nil || !duplicate.Duplicate || string(duplicate.Ciphertext) != string(release.Ciphertext) {
		t.Fatalf("idempotent proof response=%+v err=%v", duplicate, err)
	}

	var storedVerifier []byte
	if err := state.db.QueryRowContext(ctx, `SELECT secret_sha256 FROM installation_lifecycle_credentials WHERE installation_id=?`, spec.ID).Scan(&storedVerifier); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(storedVerifier), string(secret)) || len(storedVerifier) != sha256.Size {
		t.Fatal("raw lifecycle credential was persisted instead of a fixed-size verifier")
	}
}

func TestBootTrustCancelledAssignmentRejectsSecretRelease(t *testing.T) {
	state := testStore(t)
	ctx := context.Background()
	spec := telemetryInstallation(t, state, "BC:24:11:00:30:02")
	if _, err := state.SetMachinePolicy(ctx, spec.MachineID, machine.PolicyProvision, "req_policy", "test:operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := state.ArmInstallation(ctx, spec.MachineID, spec.ID, "req_arm", "test:operator"); err != nil {
		t.Fatal(err)
	}
	_, publicPEM := testBootTrustRSAKey(t)
	enrolled, _, err := state.RegisterBootTrustKey(ctx, spec.ID, publicPEM, "", "req_enroll")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.ApproveBootTrustKey(ctx, spec.MachineID, enrolled.Fingerprint, "req_approve", "test:admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := state.CancelAssignment(ctx, spec.ID, "req_cancel", "test:operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := state.CreateBootTrustChallenge(ctx, spec.ID, enrolled.Fingerprint, "req_challenge"); fault.Code(err) != fault.BootTrustEnrollmentRequired {
		t.Fatalf("cancelled assignment challenge code=%q err=%v", fault.Code(err), err)
	}
}

func testBootTrustRSAKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return privateKey, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}
