package event

import "time"

const EntityMachine = "machine"
const EntityInstallation = "installation"
const MachineDiscovered = "MACHINE_DISCOVERED"
const MachineSeen = "MACHINE_SEEN"
const MachinePolicyChanged = "MACHINE_POLICY_CHANGED"
const InstallationCreated = "INSTALLATION_CREATED"
const InstallationArmed = "INSTALLATION_ARMED"
const InstallationAssignmentCancelled = "INSTALLATION_ASSIGNMENT_CANCELLED"
const InstallationAssignmentConsumed = "INSTALLATION_ASSIGNMENT_CONSUMED"

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
