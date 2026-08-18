package store

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/assignment"
	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/installation"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
	"github.com/Ostsee-Developer/AegisPXE/internal/observability"
)

func TestAssignmentArmIsSingleActiveAuditedBinding(t *testing.T) {
	state := testStore(t)
	ctx := context.Background()
	machineRecord, _, err := state.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:30:01"}, "req_discover_assignment")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.SetMachinePolicy(ctx, machineRecord.ID, machine.PolicyProvision, "req_approve_assignment", "test:operator"); err != nil {
		t.Fatal(err)
	}

	first := createAssignmentSpec(t, state, machineRecord.ID, "req_spec_first")
	armed, err := state.ArmInstallation(ctx, machineRecord.ID, first.ID, "req_arm_first", "test:operator")
	if err != nil {
		t.Fatal(err)
	}
	if armed.State != assignment.StateArmed || armed.TrustRequirement != assignment.TrustRequirementCryptographic {
		t.Fatalf("unexpected armed assignment: %+v", armed)
	}

	second := createAssignmentSpec(t, state, machineRecord.ID, "req_spec_second")
	if _, err := state.ArmInstallation(ctx, machineRecord.ID, second.ID, "req_arm_second", "test:operator"); fault.Code(err) != fault.InstallationAssignmentConflict {
		t.Fatalf("second arm code=%q err=%v", fault.Code(err), err)
	}

	events, err := state.Events(ctx, event.EntityInstallation, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Type != event.InstallationCreated || events[1].Type != event.InstallationArmed {
		t.Fatalf("unexpected installation events: %+v", events)
	}

	cancelled, err := state.CancelAssignment(ctx, first.ID, "req_cancel_first", "test:operator")
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.State != assignment.StateCancelled || cancelled.CancelledAt.IsZero() {
		t.Fatalf("unexpected cancelled assignment: %+v", cancelled)
	}
	if _, err := state.ActiveAssignmentForMachine(ctx, machineRecord.ID); fault.Code(err) != fault.InstallationAssignmentNotFound {
		t.Fatalf("active assignment remained after cancellation: code=%q err=%v", fault.Code(err), err)
	}
	if _, err := state.ArmInstallation(ctx, machineRecord.ID, second.ID, "req_arm_second_after_cancel", "test:operator"); err != nil {
		t.Fatalf("second assignment could not arm after cancellation: %v", err)
	}
}

func TestAssignmentRejectsSpecOwnedByDifferentMachine(t *testing.T) {
	state := testStore(t)
	ctx := context.Background()
	first, _, err := state.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:30:02"}, "req_discover_a")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := state.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:30:03"}, "req_discover_b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.SetMachinePolicy(ctx, second.ID, machine.PolicyProvision, "req_approve_b", "test:operator"); err != nil {
		t.Fatal(err)
	}
	spec := createAssignmentSpec(t, state, first.ID, "req_spec_a")
	if _, err := state.ArmInstallation(ctx, second.ID, spec.ID, "req_wrong_machine", "test:operator"); fault.Code(err) != fault.InstallationAssignmentInvalid {
		t.Fatalf("machine mismatch code=%q err=%v", fault.Code(err), err)
	}
}

func TestAssignmentRejectionIsCorrelatedWithoutCredentialLeak(t *testing.T) {
	var logs bytes.Buffer
	logger := observability.New(&logs, slog.LevelDebug)
	state, err := Open(context.Background(), filepath.Join(t.TempDir(), "aegispxe.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()

	ctx := context.Background()
	machineRecord, _, err := state.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:30:04"}, "req_discover_reject")
	if err != nil {
		t.Fatal(err)
	}
	spec := createAssignmentSpec(t, state, machineRecord.ID, "req_spec_reject")
	_, err = state.ArmInstallation(ctx, machineRecord.ID, spec.ID, "req_arm_reject", "test:operator")
	if fault.Code(err) != fault.InstallationAssignmentInvalid {
		t.Fatalf("rejection code=%q err=%v", fault.Code(err), err)
	}

	logText := logs.String()
	for _, want := range []string{
		`"component":"store.assignment"`,
		`"operation":"arm"`,
		`"request_id":"req_arm_reject"`,
		`"machine_id":"` + machineRecord.ID + `"`,
		`"installation_id":"` + spec.ID + `"`,
		`"result":"rejected"`,
		`"error_code":"` + fault.InstallationAssignmentInvalid + `"`,
		`"cause":"machine_not_operator_approved"`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("assignment rejection log missing %s: %s", want, logText)
		}
	}
	if strings.Contains(logText, spec.LifecycleCredentialID) {
		t.Fatal("assignment rejection log leaked lifecycle credential identity")
	}
}

func createAssignmentSpec(t *testing.T, state *Store, machineID, requestID string) installation.Spec {
	t.Helper()
	spec, err := state.CreateInstallationSpec(context.Background(), installation.Spec{
		MachineID:             machineID,
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
		LifecycleCredentialID: "cred_assignment_test",
		CreatedBy:             "test:operator",
	}, requestID)
	if err != nil {
		t.Fatal(err)
	}
	return spec
}
