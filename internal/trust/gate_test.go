package trust

import (
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
)

func TestTrustGateKeepsSecretsBehindCryptographicProof(t *testing.T) {
	gate := Evaluate(machine.PolicyProvision, true, false)
	if !gate.PublicBootAllowed || gate.SecretReleaseAllowed || gate.Reason != "cryptographic_boot_trust_required" {
		t.Fatalf("unexpected gate: %+v", gate)
	}
}

func TestTrustGateRejectsUnapprovedScheduling(t *testing.T) {
	gate := Evaluate(machine.PolicyPending, true, true)
	if gate.PublicBootAllowed || gate.SecretReleaseAllowed || gate.Reason != "operator_approval_required" {
		t.Fatalf("unexpected gate: %+v", gate)
	}
}

func TestTrustGateAllowsSecretsOnlyWhenAllLayersPass(t *testing.T) {
	gate := Evaluate(machine.PolicyProvision, true, true)
	if !gate.PublicBootAllowed || !gate.SecretReleaseAllowed || gate.Reason != "trusted" {
		t.Fatalf("unexpected gate: %+v", gate)
	}
}
