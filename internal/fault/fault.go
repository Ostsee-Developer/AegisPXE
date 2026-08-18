package fault

import "errors"

const (
	DiscoveryRateLimited    = "PXE001_DISCOVERY_RATE_LIMITED"
	MachineIdentityConflict = "MAC001_MACHINE_IDENTITY_CONFLICT"
	MachineIdentityInvalid  = "MAC002_MACHINE_IDENTITY_INVALID"
	MachineNotFound         = "MAC003_MACHINE_NOT_FOUND"
	MachinePolicyInvalid    = "MAC004_MACHINE_POLICY_INVALID"
	ArtifactTrustFailed     = "ART001_ARTIFACT_TRUST_FAILED"
	ArtifactHashMismatch    = "ART002_ARTIFACT_HASH_MISMATCH"
	ArtifactFetchFailed     = "ART003_ARTIFACT_FETCH_FAILED"
	InstallationSpecInvalid = "INS001_INSTALLATION_SPEC_INVALID"
	InstallationNotFound    = "INS002_INSTALLATION_NOT_FOUND"
	StorageFailure          = "SYS001_STORAGE_FAILURE"
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
