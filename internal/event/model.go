package event

import "time"

const EntityMachine = "machine"
const EntityInstallation = "installation"
const EntityAgent = "agent"
const EntityOperatorUser = "operator_user"
const EntitySystem = "system"
const MachineDiscovered = "MACHINE_DISCOVERED"
const MachineSeen = "MACHINE_SEEN"
const MachinePolicyChanged = "MACHINE_POLICY_CHANGED"
const MachineNicknameChanged = "MACHINE_NICKNAME_CHANGED"
const MachineDeleted = "MACHINE_DELETED"
const InstallationCreated = "INSTALLATION_CREATED"
const InstallationDeleted = "INSTALLATION_DELETED"
const InstallationArmed = "INSTALLATION_ARMED"
const InstallationAssignmentCancelled = "INSTALLATION_ASSIGNMENT_CANCELLED"
const InstallationAssignmentConsumed = "INSTALLATION_ASSIGNMENT_CONSUMED"
const AgentCreated = "AGENT_CREATED"
const AgentBuildQueued = "AGENT_BUILD_QUEUED"
const AgentBuildStarted = "AGENT_BUILD_STARTED"
const AgentBuildReady = "AGENT_BUILD_READY"
const AgentBuildFailed = "AGENT_BUILD_FAILED"
const AgentEnrollmentCredentialCreated = "AGENT_ENROLLMENT_CREDENTIAL_CREATED"
const AgentEnrollmentCompleted = "AGENT_ENROLLMENT_COMPLETED"
const AgentCertificateIssued = "AGENT_CERTIFICATE_ISSUED"
const AgentConnected = "AGENT_CONNECTED"
const OperatorUserDiscovered = "OPERATOR_USER_DISCOVERED"
const OperatorInitialAdminClaimed = "OPERATOR_INITIAL_ADMIN_CLAIMED"
const OperatorUserApproved = "OPERATOR_USER_APPROVED"
const OperatorUserBlocked = "OPERATOR_USER_BLOCKED"
const OperatorPasskeyEnrolled = "OPERATOR_PASSKEY_ENROLLED"
const OperatorRecoveryLogin = "OPERATOR_RECOVERY_LOGIN"

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
