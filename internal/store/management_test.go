package store

import (
	"context"
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/installation"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
)

func TestMachineNicknamePersistsAndIsAudited(t *testing.T) {
	state := testStore(t)
	ctx := context.Background()
	item, _, err := state.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:50:01"}, "req_discover")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := state.SetMachineNickname(ctx, item.ID, "  Lab Node 01  ", "req_nickname", "user:test")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Nickname != "Lab Node 01" {
		t.Fatalf("nickname=%q", updated.Nickname)
	}
	loaded, err := state.Machine(ctx, item.ID)
	if err != nil || loaded.Nickname != "Lab Node 01" {
		t.Fatalf("loaded machine=%+v err=%v", loaded, err)
	}
	events, err := state.Events(ctx, event.EntityMachine, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[1].Type != event.MachineNicknameChanged || events[1].Actor != "user:test" {
		t.Fatalf("unexpected nickname audit events: %+v", events)
	}
}

func TestMachineNicknameRejectsControlCharactersAndOversize(t *testing.T) {
	state := testStore(t)
	ctx := context.Background()
	item, _, err := state.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:50:02"}, "req_discover")
	if err != nil {
		t.Fatal(err)
	}
	for _, nickname := range []string{"bad\nname", strings.Repeat("x", machine.MaxNicknameRunes+1)} {
		if _, err := state.SetMachineNickname(ctx, item.ID, nickname, "req_bad_nickname", "user:test"); fault.Code(err) != fault.MachineIdentityInvalid {
			t.Fatalf("nickname %q code=%q err=%v", nickname, fault.Code(err), err)
		}
	}
}

func TestDeleteInstallationRequiresArmedAssignmentCancellation(t *testing.T) {
	state := testStore(t)
	ctx := context.Background()
	machineRecord, spec := createManagementInstallation(t, state, "BC:24:11:00:50:03")
	if _, err := state.SetMachinePolicy(ctx, machineRecord.ID, machine.PolicyProvision, "req_policy", "user:test"); err != nil {
		t.Fatal(err)
	}
	if _, err := state.ArmInstallation(ctx, machineRecord.ID, spec.ID, "req_arm", "user:test"); err != nil {
		t.Fatal(err)
	}
	if err := state.DeleteInstallation(ctx, spec.ID, "req_delete_blocked", "user:admin"); fault.Code(err) != fault.InstallationDeleteConflict {
		t.Fatalf("armed delete code=%q err=%v", fault.Code(err), err)
	}
	if _, err := state.CancelAssignment(ctx, spec.ID, "req_cancel", "user:test"); err != nil {
		t.Fatal(err)
	}
	if err := state.DeleteInstallation(ctx, spec.ID, "req_delete", "user:admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := state.InstallationSpec(ctx, spec.ID); fault.Code(err) != fault.InstallationNotFound {
		t.Fatalf("deleted installation lookup code=%q err=%v", fault.Code(err), err)
	}
	var count int
	if err := state.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE entity_type=? AND entity_id=? AND event_type=?`, event.EntitySystem, spec.ID, event.InstallationDeleted).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("installation deletion audit count=%d", count)
	}
}

func TestDeleteMachineRequiresInstallationCleanup(t *testing.T) {
	state := testStore(t)
	ctx := context.Background()
	machineRecord, spec := createManagementInstallation(t, state, "BC:24:11:00:50:04")
	if err := state.DeleteMachine(ctx, machineRecord.ID, "req_machine_delete_blocked", "user:admin"); fault.Code(err) != fault.MachineDeleteConflict {
		t.Fatalf("machine delete code=%q err=%v", fault.Code(err), err)
	}
	if err := state.DeleteInstallation(ctx, spec.ID, "req_install_delete", "user:admin"); err != nil {
		t.Fatal(err)
	}
	if err := state.DeleteMachine(ctx, machineRecord.ID, "req_machine_delete", "user:admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := state.Machine(ctx, machineRecord.ID); fault.Code(err) != fault.MachineNotFound {
		t.Fatalf("deleted machine lookup code=%q err=%v", fault.Code(err), err)
	}
	var count int
	if err := state.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE entity_type=? AND entity_id=? AND event_type=?`, event.EntitySystem, machineRecord.ID, event.MachineDeleted).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("machine deletion audit count=%d", count)
	}
}

func createManagementInstallation(t *testing.T, state *Store, mac string) (machine.Machine, installation.Spec) {
	t.Helper()
	ctx := context.Background()
	machineRecord, _, err := state.DiscoverMachine(ctx, machine.Observation{MAC: mac}, "req_discover")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := state.CreateInstallationSpec(ctx, installation.Spec{
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
		LifecycleCredentialID: "cred_management_" + strings.TrimPrefix(machineRecord.ID, "m_"),
		CreatedBy:             "user:test",
	}, "req_install")
	if err != nil {
		t.Fatal(err)
	}
	return machineRecord, spec
}
