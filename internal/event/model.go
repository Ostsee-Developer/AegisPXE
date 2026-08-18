package event

import "time"

const EntityMachine = "machine"
const MachineDiscovered = "MACHINE_DISCOVERED"
const MachineSeen = "MACHINE_SEEN"
const MachinePolicyChanged = "MACHINE_POLICY_CHANGED"

type Event struct {
	Sequence   int64
	EntityType string
	EntityID   string
	Type       string
	OccurredAt time.Time
	RequestID  string
	Actor      string
	Message    string
	ErrorCode  string
}
