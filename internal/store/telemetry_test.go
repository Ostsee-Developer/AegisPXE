package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/installation"
	"github.com/Ostsee-Developer/AegisPXE/internal/lifecycle"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
	"github.com/Ostsee-Developer/AegisPXE/internal/telemetry"
)

func telemetryInstallation(t *testing.T, store *Store, mac string) installation.Spec {
	t.Helper()
	ctx := context.Background()
	machineRecord, _, err := store.DiscoverMachine(ctx, machine.Observation{MAC: mac}, "req_telemetry_discover")
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateInstallationSpec(ctx, installation.Spec{
		MachineID:             machineRecord.ID,
		DriverID:              "debian13",
		DriverVersion:         "1",
		OSRelease:             "13",
		Architecture:          "amd64",
		ProfileID:             "standard",
		ProfileRevision:       "rev_standard_1",
		Profile:               installationProfile(),
		Artifacts:             []installation.Artifact{installationArtifact("linux", "a"), installationArtifact("initrd.gz", "b")},
		Storage:               installation.Storage{Mode: "whole-disk", Filesystem: "ext4", TargetDisk: "/dev/vda"},
		Security:              installation.Security{AutomaticSecurityUpdates: true},
		LifecycleCredentialID: "lc_test_telemetry",
		CreatedBy:             "system:test",
	}, "req_telemetry_install")
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func TestLifecycleCredentialIssueAuthenticateAndRevoke(t *testing.T) {
	store := testStore(t)
	spec := telemetryInstallation(t, store, "BC:24:11:00:20:01")
	ctx := context.Background()

	issued, err := store.IssueLifecycleCredential(ctx, spec.ID, "req_issue", "system:test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if issued.Credential.ID != spec.LifecycleCredentialID || issued.Credential.InstallationID != spec.ID || len(issued.Secret) < 32 {
		t.Fatalf("unexpected issued credential: %+v secret_len=%d", issued.Credential, len(issued.Secret))
	}
	if _, err := store.AuthenticateLifecycleCredential(ctx, spec.ID, "wrong-secret"); fault.Code(err) != fault.InstallerCredentialInvalid {
		t.Fatalf("wrong secret code=%q err=%v", fault.Code(err), err)
	}
	if authenticated, err := store.AuthenticateLifecycleCredential(ctx, spec.ID, issued.Secret); err != nil || authenticated.ID != issued.Credential.ID {
		t.Fatalf("credential authentication failed: %+v err=%v", authenticated, err)
	}
	if _, err := store.IssueLifecycleCredential(ctx, spec.ID, "req_issue_again", "system:test", time.Hour); fault.Code(err) != fault.InstallerTelemetryConflict {
		t.Fatalf("duplicate issue code=%q err=%v", fault.Code(err), err)
	}
	if err := store.RevokeLifecycleCredential(ctx, spec.ID, "req_revoke", "system:test"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateLifecycleCredential(ctx, spec.ID, issued.Secret); fault.Code(err) != fault.InstallerCredentialExpired {
		t.Fatalf("revoked credential code=%q err=%v", fault.Code(err), err)
	}
}

func TestLifecycleEventsAreMonotonicAndIdempotent(t *testing.T) {
	store := testStore(t)
	spec := telemetryInstallation(t, store, "BC:24:11:00:20:02")
	ctx := context.Background()

	path := []struct {
		stage  lifecycle.Stage
		source lifecycle.Source
		key    string
	}{
		{lifecycle.StageQueued, lifecycle.SourceServer, "server-queued"},
		{lifecycle.StagePXEBooted, lifecycle.SourceServer, "server-pxe"},
		{lifecycle.StageInstallerStarted, lifecycle.SourceInstaller, "installer-started"},
		{lifecycle.StageDiskPreparation, lifecycle.SourceInstaller, "installer-disk"},
		{lifecycle.StageOSInstalling, lifecycle.SourceInstaller, "installer-os"},
	}
	for _, step := range path {
		accepted, duplicate, err := store.AppendLifecycleEvent(ctx, lifecycle.Report{
			InstallationID: spec.ID,
			Stage:          step.stage,
			Source:         step.source,
			IdempotencyKey: step.key,
			Message:        "stage " + string(step.stage),
		}, "req_"+step.key)
		if err != nil || duplicate || accepted.Stage != step.stage {
			t.Fatalf("stage %s rejected: duplicate=%v event=%+v err=%v", step.stage, duplicate, accepted, err)
		}
	}

	replayed, duplicate, err := store.AppendLifecycleEvent(ctx, lifecycle.Report{
		InstallationID: spec.ID,
		Stage:          lifecycle.StageOSInstalling,
		Source:         lifecycle.SourceInstaller,
		IdempotencyKey: "installer-os",
		Message:        "stage OS_INSTALLING",
	}, "req_replay")
	if err != nil || !duplicate || replayed.Stage != lifecycle.StageOSInstalling {
		t.Fatalf("idempotent replay failed: duplicate=%v event=%+v err=%v", duplicate, replayed, err)
	}

	_, _, err = store.AppendLifecycleEvent(ctx, lifecycle.Report{
		InstallationID: spec.ID,
		Stage:          lifecycle.StageHardening,
		Source:         lifecycle.SourceInstaller,
		IdempotencyKey: "skip-profile",
	}, "req_skip")
	if fault.Code(err) != fault.InstallerTelemetryConflict {
		t.Fatalf("skipped transition code=%q err=%v", fault.Code(err), err)
	}

	stage, err := store.CurrentLifecycleStage(ctx, spec.ID)
	if err != nil || stage != lifecycle.StageOSInstalling {
		t.Fatalf("unexpected current stage %q err=%v", stage, err)
	}
	events, err := store.LifecycleEvents(ctx, spec.ID, 100)
	if err != nil || len(events) != len(path)+1 {
		t.Fatalf("unexpected lifecycle events len=%d err=%v", len(events), err)
	}
	if events[0].Stage != lifecycle.StageCreated {
		t.Fatalf("installation creation did not seed lifecycle: %+v", events)
	}
}

func TestTerminalLifecycleRevokesCredential(t *testing.T) {
	store := testStore(t)
	spec := telemetryInstallation(t, store, "BC:24:11:00:20:03")
	ctx := context.Background()
	issued, err := store.IssueLifecycleCredential(ctx, spec.ID, "req_issue", "system:test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		stage  lifecycle.Stage
		source lifecycle.Source
	}{
		{lifecycle.StageQueued, lifecycle.SourceServer},
		{lifecycle.StagePXEBooted, lifecycle.SourceServer},
		{lifecycle.StageInstallerStarted, lifecycle.SourceInstaller},
		{lifecycle.StageDiskPreparation, lifecycle.SourceInstaller},
		{lifecycle.StageOSInstalling, lifecycle.SourceInstaller},
		{lifecycle.StageProfileApplying, lifecycle.SourceInstaller},
		{lifecycle.StageHardening, lifecycle.SourceInstaller},
		{lifecycle.StageValidating, lifecycle.SourceValidator},
		{lifecycle.StageSuccess, lifecycle.SourceValidator},
	}
	for index, step := range steps {
		_, _, err := store.AppendLifecycleEvent(ctx, lifecycle.Report{
			InstallationID: spec.ID,
			Stage:          step.stage,
			Source:         step.source,
			IdempotencyKey: "terminal-" + string(rune('a'+index)),
		}, "req_terminal")
		if err != nil {
			t.Fatalf("stage %s: %v", step.stage, err)
		}
	}
	if _, err := store.AuthenticateLifecycleCredential(ctx, spec.ID, issued.Secret); fault.Code(err) != fault.InstallerCredentialExpired {
		t.Fatalf("terminal credential code=%q err=%v", fault.Code(err), err)
	}
}

func TestInstallationLogChunksAreRedactedSequencedAndIdempotent(t *testing.T) {
	store := testStore(t)
	spec := telemetryInstallation(t, store, "BC:24:11:00:20:04")
	ctx := context.Background()

	first, duplicate, err := store.AppendInstallationLogChunk(ctx, telemetry.LogChunk{
		InstallationID: spec.ID,
		Sequence:       1,
		Source:         lifecycle.SourceInstaller,
		RequestID:      "req_log_1",
		IdempotencyKey: "log-1",
		Content:        "normal line\ntoken=do-not-store\nafter",
	})
	if err != nil || duplicate {
		t.Fatalf("first log rejected: duplicate=%v err=%v", duplicate, err)
	}
	if strings.Contains(first.Content, "do-not-store") || !strings.Contains(first.Content, "[REDACTED") {
		t.Fatalf("sensitive log content was not redacted: %q", first.Content)
	}

	replayed, duplicate, err := store.AppendInstallationLogChunk(ctx, telemetry.LogChunk{
		InstallationID: spec.ID,
		Sequence:       1,
		Source:         lifecycle.SourceInstaller,
		RequestID:      "req_log_replay",
		IdempotencyKey: "log-1",
		Content:        "normal line\ntoken=do-not-store\nafter",
	})
	if err != nil || !duplicate || replayed.Digest != first.Digest {
		t.Fatalf("log replay failed: duplicate=%v err=%v", duplicate, err)
	}

	_, _, err = store.AppendInstallationLogChunk(ctx, telemetry.LogChunk{
		InstallationID: spec.ID,
		Sequence:       3,
		Source:         lifecycle.SourceInstaller,
		RequestID:      "req_log_gap",
		IdempotencyKey: "log-3",
		Content:        "gap",
	})
	if fault.Code(err) != fault.InstallerTelemetryConflict {
		t.Fatalf("log gap code=%q err=%v", fault.Code(err), err)
	}

	if _, _, err := store.AppendInstallationLogChunk(ctx, telemetry.LogChunk{
		InstallationID: spec.ID,
		Sequence:       2,
		Source:         lifecycle.SourceInstaller,
		RequestID:      "req_log_2",
		IdempotencyKey: "log-2",
		Content:        "second",
	}); err != nil {
		t.Fatal(err)
	}
	chunks, err := store.InstallationLogChunks(ctx, spec.ID, 10)
	if err != nil || len(chunks) != 2 || chunks[0].Sequence != 1 || chunks[1].Sequence != 2 {
		t.Fatalf("unexpected stored log chunks: %+v err=%v", chunks, err)
	}
}
