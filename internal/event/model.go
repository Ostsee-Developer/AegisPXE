package event

import "time"

const (
	EntityMachine      = "machine"
	MachineDiscovered = "MACHINE_DISCOVERED"
	MachineSeen       = "MACHINE_SEEN"
)

type Event struct {
	Sequence   int64
	EntityType string
	EntityID   string
	Type       string
	OccurredAt time.Time
	RequestID  string
	Message    string
	ErrorCode  string
}
