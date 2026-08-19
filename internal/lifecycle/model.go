package lifecycle

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

type Stage string

type Source string

const (
	StageCreated          Stage = "CREATED"
	StageQueued           Stage = "QUEUED"
	StagePXEBooted        Stage = "PXE_BOOTED"
	StageInstallerStarted Stage = "INSTALLER_STARTED"
	StageDiskPreparation  Stage = "DISK_PREPARATION"
	StageOSInstalling     Stage = "OS_INSTALLING"
	StageProfileApplying  Stage = "PROFILE_APPLYING"
	StageHardening        Stage = "HARDENING"
	StageFirstBoot        Stage = "FIRST_BOOT"
	StageValidating       Stage = "VALIDATING"
	StageSuccess          Stage = "SUCCESS"
	StageFailed           Stage = "FAILED"
)

const (
	SourceServer    Source = "server"
	SourceInstaller Source = "installer"
	SourceFinalizer Source = "finalizer"
	SourceValidator Source = "validator"
)

var idempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type Event struct {
	Sequence       int64
	InstallationID string
	Stage          Stage
	Source         Source
	ReceivedAt     time.Time
	ClientAt       time.Time
	RequestID      string
	IdempotencyKey string
	Message        string
	ErrorCode      string
	Metadata       map[string]string
}

type Report struct {
	InstallationID string
	Stage          Stage
	Source         Source
	ClientAt       time.Time
	IdempotencyKey string
	Message        string
	ErrorCode      string
	Metadata       map[string]string
}

func (s Stage) Valid() bool {
	switch s {
	case StageCreated, StageQueued, StagePXEBooted, StageInstallerStarted, StageDiskPreparation,
		StageOSInstalling, StageProfileApplying, StageHardening, StageFirstBoot, StageValidating,
		StageSuccess, StageFailed:
		return true
	default:
		return false
	}
}

func (s Stage) Terminal() bool {
	return s == StageSuccess || s == StageFailed
}

func (s Source) Valid() bool {
	switch s {
	case SourceServer, SourceInstaller, SourceFinalizer, SourceValidator:
		return true
	default:
		return false
	}
}

func ValidateIdempotencyKey(value string) error {
	if value != strings.TrimSpace(value) || !idempotencyPattern.MatchString(value) {
		return errors.New("idempotency key is invalid")
	}
	return nil
}

func (r Report) Validate() error {
	if strings.TrimSpace(r.InstallationID) == "" {
		return errors.New("installation ID is required")
	}
	if !r.Stage.Valid() {
		return errors.New("lifecycle stage is invalid")
	}
	if !r.Source.Valid() {
		return errors.New("lifecycle source is invalid")
	}
	if err := ValidateIdempotencyKey(r.IdempotencyKey); err != nil {
		return err
	}
	if len(r.Message) > 1024 || len(r.ErrorCode) > 128 {
		return errors.New("lifecycle event text exceeds size limit")
	}
	if r.Stage == StageFailed && strings.TrimSpace(r.ErrorCode) == "" {
		return errors.New("failed lifecycle event requires an error code")
	}
	if r.Stage != StageFailed && strings.TrimSpace(r.ErrorCode) != "" {
		return errors.New("non-failed lifecycle event cannot carry an error code")
	}
	if len(r.Metadata) > 32 {
		return errors.New("too many lifecycle metadata fields")
	}
	for key, value := range r.Metadata {
		key = strings.TrimSpace(key)
		if key == "" || len(key) > 64 || len(value) > 512 {
			return errors.New("lifecycle metadata exceeds size limit")
		}
		if sensitiveMetadataKey(key) {
			return errors.New("lifecycle metadata contains a sensitive key")
		}
	}
	if !SourceAllowed(r.Stage, r.Source) {
		return errors.New("lifecycle source is not authorized for stage")
	}
	return nil
}

func SourceAllowed(stage Stage, source Source) bool {
	switch stage {
	case StageCreated, StageQueued, StagePXEBooted:
		return source == SourceServer
	case StageInstallerStarted, StageDiskPreparation, StageOSInstalling, StageProfileApplying, StageHardening:
		return source == SourceInstaller
	case StageFirstBoot:
		return source == SourceFinalizer
	case StageValidating, StageSuccess:
		return source == SourceValidator || source == SourceFinalizer
	case StageFailed:
		return source == SourceInstaller || source == SourceFinalizer || source == SourceValidator
	default:
		return false
	}
}

func CanAdvance(current, next Stage) bool {
	if !next.Valid() {
		return false
	}
	if current == "" {
		return next == StageCreated || next == StageQueued || next == StagePXEBooted
	}
	if current.Terminal() {
		return false
	}
	if next == StageFailed {
		switch current {
		case StagePXEBooted, StageInstallerStarted, StageDiskPreparation, StageOSInstalling, StageProfileApplying, StageHardening, StageFirstBoot, StageValidating:
			return true
		default:
			return false
		}
	}
	if current == next {
		return false
	}
	switch current {
	case StageCreated:
		return next == StageQueued || next == StagePXEBooted
	case StageQueued:
		return next == StagePXEBooted
	case StagePXEBooted:
		return next == StageInstallerStarted
	case StageInstallerStarted:
		return next == StageDiskPreparation
	case StageDiskPreparation:
		return next == StageOSInstalling
	case StageOSInstalling:
		return next == StageProfileApplying
	case StageProfileApplying:
		return next == StageHardening
	case StageHardening:
		return next == StageFirstBoot || next == StageValidating
	case StageFirstBoot:
		return next == StageValidating
	case StageValidating:
		return next == StageSuccess
	default:
		return false
	}
}

func sensitiveMetadataKey(key string) bool {
	value := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), ".", "_"))
	for _, part := range []string{"token", "password", "secret", "authorization", "cookie", "credential", "recovery_key", "private_key"} {
		if strings.Contains(value, part) {
			return true
		}
	}
	return false
}
