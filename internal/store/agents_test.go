package store

import (
	"context"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/agent"
	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
)

func TestCreateManagedAgentBindsOneAgentToInstallationAndAudits(t *testing.T) {
	state := testStore(t)
	ctx := context.Background()
	machineRecord, _, err := state.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:40:01"}, "req_agent_machine")
	if err != nil {
		t.Fatal(err)
	}
	spec := createAssignmentSpec(t, state, machineRecord.ID, "req_agent_spec")

	record, err := state.CreateManagedAgent(ctx, spec.ID, []string{"service.status", " diagnostics.read ", "service.status"}, agent.UpdateModeAutomatic, "req_agent_create", "test:operator")
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.ValidateID(record.ID); err != nil {
		t.Fatalf("agent ID is invalid: %v", err)
	}
	if record.InstallationID != spec.ID || record.MachineID != machineRecord.ID {
		t.Fatalf("unexpected agent binding: %+v", record)
	}
	if record.State != agent.StatePendingBuild || record.UpdateMode != agent.UpdateModeAutomatic || record.DesiredGeneration != 1 || record.ActiveGeneration != 0 {
		t.Fatalf("unexpected initial managed agent state: %+v", record)
	}
	if len(record.CapabilityCeiling) != 2 || record.CapabilityCeiling[0] != "diagnostics.read" || record.CapabilityCeiling[1] != "service.status" {
		t.Fatalf("unexpected capability ceiling: %v", record.CapabilityCeiling)
	}

	byInstallation, err := state.ManagedAgentByInstallation(ctx, spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if byInstallation.ID != record.ID {
		t.Fatalf("agent lookup returned %q want %q", byInstallation.ID, record.ID)
	}
	events, err := state.Events(ctx, event.EntityAgent, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != event.AgentCreated || events[0].RequestID != "req_agent_create" {
		t.Fatalf("unexpected agent events: %+v", events)
	}
}

func TestCreateManagedAgentRejectsSecondAgentForInstallation(t *testing.T) {
	state := testStore(t)
	ctx := context.Background()
	machineRecord, _, err := state.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:40:02"}, "req_agent_machine")
	if err != nil {
		t.Fatal(err)
	}
	spec := createAssignmentSpec(t, state, machineRecord.ID, "req_agent_spec")
	if _, err := state.CreateManagedAgent(ctx, spec.ID, nil, agent.UpdateModeManual, "req_agent_first", "test:operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := state.CreateManagedAgent(ctx, spec.ID, nil, agent.UpdateModeManual, "req_agent_second", "test:operator"); fault.Code(err) != fault.AgentConflict {
		t.Fatalf("second agent code=%q err=%v", fault.Code(err), err)
	}
}

func TestCreateManagedAgentRejectsGenericShellCapability(t *testing.T) {
	state := testStore(t)
	ctx := context.Background()
	machineRecord, _, err := state.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:40:03"}, "req_agent_machine")
	if err != nil {
		t.Fatal(err)
	}
	spec := createAssignmentSpec(t, state, machineRecord.ID, "req_agent_spec")
	if _, err := state.CreateManagedAgent(ctx, spec.ID, []string{"shell"}, agent.UpdateModeManual, "req_agent_invalid", "test:operator"); fault.Code(err) != fault.AgentInvalid {
		t.Fatalf("invalid capability code=%q err=%v", fault.Code(err), err)
	}
	if _, err := state.ManagedAgentByInstallation(ctx, spec.ID); fault.Code(err) != fault.AgentNotFound {
		t.Fatalf("agent unexpectedly persisted after invalid capability: code=%q err=%v", fault.Code(err), err)
	}
}
