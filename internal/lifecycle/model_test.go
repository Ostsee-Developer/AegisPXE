package lifecycle

import "testing"

func TestLifecycleHappyPath(t *testing.T) {
	path := []Stage{
		StageCreated,
		StageQueued,
		StagePXEBooted,
		StageInstallerStarted,
		StageDiskPreparation,
		StageOSInstalling,
		StageProfileApplying,
		StageHardening,
		StageFirstBoot,
		StageValidating,
		StageSuccess,
	}
	current := Stage("")
	for _, next := range path {
		if !CanAdvance(current, next) {
			t.Fatalf("expected transition %q -> %q", current, next)
		}
		current = next
	}
	if CanAdvance(StageSuccess, StageFailed) {
		t.Fatal("terminal success must not transition")
	}
}

func TestLifecycleAllowsValidationWithoutFirstBoot(t *testing.T) {
	if !CanAdvance(StageHardening, StageValidating) {
		t.Fatal("hardening should be able to enter validation directly")
	}
}

func TestLifecycleFailureRequiresRuntimeStageAndErrorCode(t *testing.T) {
	if CanAdvance(StageCreated, StageFailed) {
		t.Fatal("created installation must not fail as installer runtime telemetry")
	}
	if !CanAdvance(StageOSInstalling, StageFailed) {
		t.Fatal("active installer stage should allow failure")
	}
	report := Report{
		InstallationID: "i_test",
		Stage:          StageFailed,
		Source:         SourceInstaller,
		IdempotencyKey: "evt-failed-1",
	}
	if err := report.Validate(); err == nil {
		t.Fatal("failed event without error code was accepted")
	}
	report.ErrorCode = "INS900_TEST_FAILURE"
	if err := report.Validate(); err != nil {
		t.Fatalf("valid failed event rejected: %v", err)
	}
}

func TestLifecycleRejectsUnauthorizedSourceAndSensitiveMetadata(t *testing.T) {
	report := Report{
		InstallationID: "i_test",
		Stage:          StageOSInstalling,
		Source:         SourceServer,
		IdempotencyKey: "evt-os-1",
	}
	if err := report.Validate(); err == nil {
		t.Fatal("server source was accepted for installer stage")
	}
	report.Source = SourceInstaller
	report.Metadata = map[string]string{"authorization_token": "must-never-land-here"}
	if err := report.Validate(); err == nil {
		t.Fatal("sensitive metadata key was accepted")
	}
}

func TestLifecycleIdempotencyKeyValidation(t *testing.T) {
	for _, value := range []string{"evt-1", "installer:stage.2", "abc_DEF"} {
		if err := ValidateIdempotencyKey(value); err != nil {
			t.Fatalf("valid idempotency key %q rejected: %v", value, err)
		}
	}
	for _, value := range []string{"", " has-space", "slash/nope"} {
		if err := ValidateIdempotencyKey(value); err == nil {
			t.Fatalf("invalid idempotency key %q accepted", value)
		}
	}
}
