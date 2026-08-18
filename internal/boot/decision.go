package boot

import "github.com/Ostsee-Developer/AegisPXE/internal/machine"

type Action string

const (
	ActionLocal   Action = "local"
	ActionBlocked Action = "blocked"
)

type Decision struct {
	Action Action `json:"action"`
	Reason string `json:"reason"`
}

func Decide(policy machine.Policy) Decision {
	switch policy {
	case machine.PolicyBlocked:
		return Decision{Action: ActionBlocked, Reason: "machine_blocked"}
	case machine.PolicyLocal:
		return Decision{Action: ActionLocal, Reason: "local_policy"}
	case machine.PolicyProvision:
		return Decision{Action: ActionLocal, Reason: "installation_not_armed"}
	case machine.PolicyPending:
		fallthrough
	default:
		return Decision{Action: ActionLocal, Reason: "pending_approval"}
	}
}
