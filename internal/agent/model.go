package agent

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const MaxCapabilityCeiling = 32

var (
	uuidV4Pattern     = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	capabilityPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:\.[a-z][a-z0-9_-]*)+$`)
)

type State string

const (
	StatePendingBuild State = "pending_build"
	StateReady        State = "ready"
	StateUnenrolled   State = "unenrolled"
	StateEnrolling    State = "enrolling"
	StateOnline       State = "online"
	StateDegraded     State = "degraded"
	StateOffline      State = "offline"
	StateRevoked      State = "revoked"
)

func (s State) Valid() bool {
	switch s {
	case StatePendingBuild, StateReady, StateUnenrolled, StateEnrolling, StateOnline, StateDegraded, StateOffline, StateRevoked:
		return true
	default:
		return false
	}
}

type BuildState string

const (
	BuildStateQueued     BuildState = "queued"
	BuildStateBuilding   BuildState = "building"
	BuildStateReady      BuildState = "ready"
	BuildStateFailed     BuildState = "failed"
	BuildStateSuperseded BuildState = "superseded"
	BuildStateRevoked    BuildState = "revoked"
)

func (s BuildState) Valid() bool {
	switch s {
	case BuildStateQueued, BuildStateBuilding, BuildStateReady, BuildStateFailed, BuildStateSuperseded, BuildStateRevoked:
		return true
	default:
		return false
	}
}

type UpdateMode string

const (
	UpdateModeManual    UpdateMode = "manual"
	UpdateModeAutomatic UpdateMode = "automatic"
)

func (m UpdateMode) Valid() bool {
	return m == UpdateModeManual || m == UpdateModeAutomatic
}

type UpdateState string

const (
	UpdateStateIdle        UpdateState = "idle"
	UpdateStateAvailable   UpdateState = "available"
	UpdateStateDownloading UpdateState = "downloading"
	UpdateStateVerifying   UpdateState = "verifying"
	UpdateStateStaging     UpdateState = "staging"
	UpdateStateInstalling  UpdateState = "installing"
	UpdateStateRestarting  UpdateState = "restarting"
	UpdateStateConfirming  UpdateState = "confirming"
	UpdateStateSuccess     UpdateState = "success"
	UpdateStateFailed      UpdateState = "failed"
	UpdateStateRollback    UpdateState = "rollback"
)

func (s UpdateState) Valid() bool {
	switch s {
	case UpdateStateIdle, UpdateStateAvailable, UpdateStateDownloading, UpdateStateVerifying, UpdateStateStaging, UpdateStateInstalling, UpdateStateRestarting, UpdateStateConfirming, UpdateStateSuccess, UpdateStateFailed, UpdateStateRollback:
		return true
	default:
		return false
	}
}

type Record struct {
	ID                string
	InstallationID    string
	MachineID         string
	State             State
	UpdateMode        UpdateMode
	UpdateState       UpdateState
	CapabilityCeiling []string
	ActiveGeneration  int
	DesiredGeneration int
	ActiveVersion     string
	LastSeenAt        time.Time
	LastHeartbeatJSON string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	RevokedAt         time.Time
}

func (r Record) Validate() error {
	if err := ValidateID(r.ID); err != nil {
		return err
	}
	if strings.TrimSpace(r.InstallationID) == "" || strings.TrimSpace(r.MachineID) == "" {
		return errors.New("installation ID and machine ID are required")
	}
	if len(r.InstallationID) > 128 || len(r.MachineID) > 128 {
		return errors.New("agent binding metadata exceeds size limit")
	}
	if !r.State.Valid() {
		return fmt.Errorf("invalid agent state %q", r.State)
	}
	if !r.UpdateMode.Valid() {
		return fmt.Errorf("invalid agent update mode %q", r.UpdateMode)
	}
	if !r.UpdateState.Valid() {
		return fmt.Errorf("invalid agent update state %q", r.UpdateState)
	}
	if err := validateCapabilityCeiling(r.CapabilityCeiling); err != nil {
		return err
	}
	if r.ActiveGeneration < 0 || r.DesiredGeneration < 1 {
		return errors.New("agent generations are invalid")
	}
	if len(r.ActiveVersion) > 128 {
		return errors.New("agent version exceeds size limit")
	}
	if r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() || r.UpdatedAt.Before(r.CreatedAt) {
		return errors.New("agent timestamps are invalid")
	}
	if r.State == StateRevoked && r.RevokedAt.IsZero() {
		return errors.New("revoked agent requires revocation time")
	}
	if r.State != StateRevoked && !r.RevokedAt.IsZero() {
		return errors.New("active agent cannot have revocation time")
	}
	return nil
}

func (r Record) Clone() Record {
	copy := r
	copy.CapabilityCeiling = append([]string(nil), r.CapabilityCeiling...)
	return copy
}

type Build struct {
	ID                string
	AgentID           string
	Generation        int
	Version           string
	Architecture      string
	CapabilityCeiling []string
	State             BuildState
	PackagePath       string
	PackageSHA256     string
	PackageSize       int64
	ManifestSHA256    string
	CreatedAt         time.Time
	StartedAt         time.Time
	ReadyAt           time.Time
	FailedAt          time.Time
	SupersededAt      time.Time
	RevokedAt         time.Time
	ErrorCode         string
	ErrorMessage      string
}

func (b Build) Clone() Build {
	copy := b
	copy.CapabilityCeiling = append([]string(nil), b.CapabilityCeiling...)
	return copy
}

func ValidateID(value string) error {
	if value != strings.TrimSpace(value) || !uuidV4Pattern.MatchString(value) {
		return errors.New("agent ID must be a canonical lowercase UUIDv4")
	}
	return nil
}

func NormalizeCapabilityCeiling(values []string) ([]string, error) {
	if len(values) > MaxCapabilityCeiling {
		return nil, fmt.Errorf("capability ceiling exceeds %d entries", MaxCapabilityCeiling)
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if len(value) == 0 || len(value) > 64 || !capabilityPattern.MatchString(value) {
			return nil, fmt.Errorf("invalid agent capability %q", raw)
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out, nil
}

func validateCapabilityCeiling(values []string) error {
	if len(values) > MaxCapabilityCeiling {
		return fmt.Errorf("capability ceiling exceeds %d entries", MaxCapabilityCeiling)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if len(value) == 0 || len(value) > 64 || !capabilityPattern.MatchString(value) {
			return fmt.Errorf("invalid agent capability %q", value)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("duplicate agent capability %q", value)
		}
		seen[value] = struct{}{}
	}
	return nil
}
