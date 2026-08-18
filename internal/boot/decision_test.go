package boot

import (
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
)

func TestDecideNeverProvisionsWithoutArmedSpec(t *testing.T) {
	tests := []struct {
		policy machine.Policy
		action Action
		reason string
	}{
		{machine.PolicyPending, ActionLocal, "pending_approval"},
		{machine.PolicyLocal, ActionLocal, "local_policy"},
		{machine.PolicyProvision, ActionLocal, "installation_not_armed"},
		{machine.PolicyBlocked, ActionBlocked, "machine_blocked"},
	}
	for _, tt := range tests {
		t.Run(string(tt.policy), func(t *testing.T) {
			decision := Decide(tt.policy)
			if decision.Action != tt.action || decision.Reason != tt.reason {
				t.Fatalf("Decide(%s) = %+v, want action=%s reason=%s", tt.policy, decision, tt.action, tt.reason)
			}
		})
	}
}
