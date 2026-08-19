package store

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "aegispxe.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestDiscoverMachineIsIdempotent(t *testing.T) {
	store := testStore(t)
	obs := machine.Observation{MAC: "BC:24:11:AA:BB:CC", SMBIOSUUID: "11111111-1111-1111-1111-111111111111", Architecture: "amd64", Firmware: "uefi"}

	first, created, err := store.DiscoverMachine(context.Background(), obs, "req_first")
	if err != nil || !created {
		t.Fatalf("first discovery: created=%v err=%v", created, err)
	}
	second, created, err := store.DiscoverMachine(context.Background(), obs, "req_second")
	if err != nil || created || second.ID != first.ID || second.Policy != machine.PolicyPending {
		t.Fatalf("repeat discovery changed identity: first=%+v second=%+v created=%v err=%v", first, second, created, err)
	}

	events, err := store.Events(context.Background(), event.EntityMachine, first.ID)
	if err != nil || len(events) != 2 || events[0].Type != event.MachineDiscovered || events[1].Type != event.MachineSeen {
		t.Fatalf("unexpected event timeline: %+v err=%v", events, err)
	}
}

func TestRecentEventsReturnsBoundedChronologicalTail(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	obs := machine.Observation{MAC: "BC:24:11:AA:BB:DD", Architecture: "amd64", Firmware: "uefi"}
	first, _, err := store.DiscoverMachine(ctx, obs, "req_1")
	if err != nil {
		t.Fatal(err)
	}
	for _, requestID := range []string{"req_2", "req_3", "req_4", "req_5"} {
		if _, _, err := store.DiscoverMachine(ctx, obs, requestID); err != nil {
			t.Fatal(err)
		}
	}
	recent, err := store.RecentEvents(ctx, event.EntityMachine, first.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 3 || recent[0].RequestID != "req_3" || recent[2].RequestID != "req_5" {
		t.Fatalf("unexpected recent event tail: %+v", recent)
	}
	if recent[0].Sequence >= recent[1].Sequence || recent[1].Sequence >= recent[2].Sequence {
		t.Fatalf("recent events are not chronological: %+v", recent)
	}
}

func TestDiscoverMachineRejectsIdentityConflict(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	_, _, _ = store.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:00:01", SMBIOSUUID: "11111111-1111-1111-1111-111111111111"}, "req_a")
	_, _, _ = store.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:00:02", SMBIOSUUID: "22222222-2222-2222-2222-222222222222"}, "req_b")

	_, _, err := store.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:00:01", SMBIOSUUID: "22222222-2222-2222-2222-222222222222"}, "req_conflict")
	if fault.Code(err) != fault.MachineIdentityConflict {
		t.Fatalf("conflict code=%q err=%v", fault.Code(err), err)
	}
}

func TestMachinePolicyChangeIsAudited(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	machineRecord, _, err := store.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:00:03"}, "req_discover")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetMachinePolicy(ctx, machineRecord.ID, machine.PolicyLocal, "req_policy", "admin:test"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetMachinePolicy(ctx, machineRecord.ID, machine.PolicyLocal, "req_noop", "admin:test"); err != nil {
		t.Fatal(err)
	}

	events, err := store.Events(ctx, event.EntityMachine, machineRecord.ID)
	if err != nil || len(events) != 2 || events[1].Type != event.MachinePolicyChanged || events[1].Actor != "admin:test" {
		t.Fatalf("unexpected policy audit timeline: %+v err=%v", events, err)
	}
}
