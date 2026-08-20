package fault

import "errors"

const (
	DiscoveryRateLimited            = "PXE001_DISCOVERY_RATE_LIMITED"
	MachineIdentityConflict         = "MAC001_MACHINE_IDENTITY_CONFLICT"
	MachineIdentityInvalid          = "MAC002_MACHINE_IDENTITY_INVALID"
	MachineNotFound                 = "MAC003_MACHINE_NOT_FOUND"
	MachinePolicyInvalid            = "MAC004_MACHINE_POLICY_INVALID"
	MachineDeleteConflict           = "MAC005_MACHINE_DELETE_CONFLICT"
	ArtifactTrustFailed             = "ART001_ARTIFACT_TRUST_FAILED"
	ArtifactHashMismatch            = "ART002_ARTIFACT_HASH_MISMATCH"
	ArtifactFetchFailed             = "ART003_ARTIFACT_FETCH_FAILED"
	DriverSpecUnsupported           = "DRV001_DRIVER_SPEC_UNSUPPORTED"
	DriverRenderFailed              = "DRV002_DRIVER_RENDER_FAILED"
	InstallationSpecInvalid         = "INS001_INSTALLATION_SPEC_INVALID"
	InstallationNotFound            = "INS002_INSTALLATION_NOT_FOUND"
	InstallationAssignmentInvalid   = "INS003_INSTALLATION_ASSIGNMENT_INVALID"
	InstallationAssignmentConflict  = "INS004_INSTALLATION_ASSIGNMENT_CONFLICT"
	InstallationAssignmentNotFound  = "INS005_INSTALLATION_ASSIGNMENT_NOT_FOUND"
	InstallerTelemetryInvalid       = "INS006_INSTALLER_TELEMETRY_INVALID"
	InstallerTelemetryConflict      = "INS007_INSTALLER_TELEMETRY_CONFLICT"
	InstallerLogLimitExceeded       = "INS008_INSTALLER_LOG_LIMIT_EXCEEDED"
	InstallationDeleteConflict      = "INS009_INSTALLATION_DELETE_CONFLICT"
	CryptographicBootTrustRequired  = "SEC001_CRYPTOGRAPHIC_BOOT_TRUST_REQUIRED"
	OperatorAuthenticationFailed    = "SEC002_OPERATOR_AUTHENTICATION_FAILED"
	OperatorAuthRateLimited         = "SEC003_OPERATOR_AUTH_RATE_LIMITED"
	OperatorSessionRequired         = "SEC004_OPERATOR_SESSION_REQUIRED"
	OperatorCSRFInvalid             = "SEC005_OPERATOR_CSRF_INVALID"
	OperatorSecureTransportRequired = "SEC006_SECURE_OPERATOR_TRANSPORT_REQUIRED"
	OperatorUserPending             = "SEC007_OPERATOR_USER_PENDING_REVIEW"
	OperatorUserBlocked             = "SEC008_OPERATOR_USER_BLOCKED"
	OperatorUserNotFound            = "SEC009_OPERATOR_USER_NOT_FOUND"
	OperatorPasskeyRequired         = "SEC010_OPERATOR_PASSKEY_REQUIRED"
	OperatorPasskeyFailed           = "SEC011_OPERATOR_PASSKEY_FAILED"
	OperatorAuthorizationDenied     = "SEC012_OPERATOR_AUTHORIZATION_DENIED"
	OperatorRecoveryFailed          = "SEC013_OPERATOR_RECOVERY_FAILED"
	OperatorWebAuthnNotConfigured   = "SEC014_OPERATOR_WEBAUTHN_NOT_CONFIGURED"
	InstallerCredentialRequired     = "SEC015_INSTALLER_CREDENTIAL_REQUIRED"
	InstallerCredentialInvalid      = "SEC016_INSTALLER_CREDENTIAL_INVALID"
	InstallerCredentialExpired      = "SEC017_INSTALLER_CREDENTIAL_EXPIRED"
	BootTrustEnrollmentRequired     = "SEC018_BOOT_TRUST_ENROLLMENT_REQUIRED"
	BootTrustKeyInvalid             = "SEC019_BOOT_TRUST_KEY_INVALID"
	BootTrustProofInvalid           = "SEC020_BOOT_TRUST_PROOF_INVALID"
	BootTrustChallengeExpired       = "SEC021_BOOT_TRUST_CHALLENGE_EXPIRED"
	BootTrustReplayRejected         = "SEC022_BOOT_TRUST_REPLAY_REJECTED"
	SecureBootRequired              = "SEC023_SECURE_BOOT_REQUIRED"
	SecureBootEvidenceInvalid       = "SEC024_SECURE_BOOT_EVIDENCE_INVALID"
	SecureBootAssetsInvalid         = "SEC025_SECURE_BOOT_ASSETS_INVALID"
	AgentNotFound                   = "AGT001_AGENT_NOT_FOUND"
	AgentConflict                   = "AGT002_AGENT_CONFLICT"
	AgentInvalid                    = "AGT003_AGENT_INVALID"
	AgentBuildNotReady              = "AGT004_AGENT_BUILD_NOT_READY"
	AgentBuildNotFound              = "AGT005_AGENT_BUILD_NOT_FOUND"
	AgentBuildInvalid               = "AGT006_AGENT_BUILD_INVALID"
	AgentBuildQueueEmpty            = "AGT007_AGENT_BUILD_QUEUE_EMPTY"
	AgentBuildFailed                = "AGT008_AGENT_BUILD_FAILED"
	AgentTrustUnavailable           = "AGT009_AGENT_TRUST_UNAVAILABLE"
	AgentBuilderNotConfigured       = "AGT010_AGENT_BUILDER_NOT_CONFIGURED"
	AgentEnrollmentRequired         = "AGT011_AGENT_ENROLLMENT_REQUIRED"
	AgentEnrollmentInvalid          = "AGT012_AGENT_ENROLLMENT_INVALID"
	AgentEnrollmentExpired          = "AGT013_AGENT_ENROLLMENT_EXPIRED"
	AgentEnrollmentReplay           = "AGT014_AGENT_ENROLLMENT_REPLAY_REJECTED"
	AgentCertificateInvalid         = "AGT015_AGENT_CERTIFICATE_INVALID"
	AgentCertificateRevoked         = "AGT016_AGENT_CERTIFICATE_REVOKED"
	AgentHeartbeatInvalid           = "AGT017_AGENT_HEARTBEAT_INVALID"
	StorageFailure                  = "SYS001_STORAGE_FAILURE"
)

type Error struct {
	Code    string
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

func (e *Error) Unwrap() error { return e.Err }

func New(code, message string, err error) error {
	return &Error{Code: code, Message: message, Err: err}
}

func Code(err error) string {
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return ""
}
