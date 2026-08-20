package store

import (
	"context"
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/agent"
	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
)

const testManifestSignature = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func TestInstallationAutomaticallyCreatesManagedAgentAndQueuesBuild(t *testing.T) {
	state := testStore(t)
	ctx := context.Background()
	machineRecord, _, err := state.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:40:01"}, "req_agent_machine")
	if err != nil { t.Fatal(err) }
	spec := createAssignmentSpecWithoutReadyAgent(t, state, machineRecord.ID, "req_agent_spec")
	record, err := state.ManagedAgentByInstallation(ctx, spec.ID)
	if err != nil { t.Fatal(err) }
	if err := agent.ValidateID(record.ID); err != nil { t.Fatalf("agent ID is invalid: %v", err) }
	if record.InstallationID != spec.ID || record.MachineID != machineRecord.ID { t.Fatalf("unexpected agent binding: %+v", record) }
	if record.State != agent.StatePendingBuild || record.UpdateMode != agent.UpdateModeManual || record.DesiredGeneration != 1 || record.ActiveGeneration != 0 { t.Fatalf("unexpected initial managed agent state: %+v", record) }
	if len(record.CapabilityCeiling) != 0 { t.Fatalf("unexpected initial capability ceiling: %v", record.CapabilityCeiling) }
	build, err := state.AgentBuild(ctx, record.ID, 1)
	if err != nil { t.Fatal(err) }
	if build.State != agent.BuildStateQueued || build.Generation != 1 || build.Architecture != spec.Architecture { t.Fatalf("unexpected initial build: %+v", build) }
	events, err := state.Events(ctx, event.EntityAgent, record.ID)
	if err != nil { t.Fatal(err) }
	if len(events) != 2 || events[0].Type != event.AgentCreated || events[1].Type != event.AgentBuildQueued || events[0].RequestID != "req_agent_spec" || events[1].RequestID != "req_agent_spec" { t.Fatalf("unexpected agent events: %+v", events) }
}

func TestCreateManagedAgentRejectsSecondAgentForInstallation(t *testing.T) {
	state := testStore(t)
	ctx := context.Background()
	machineRecord, _, err := state.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:40:02"}, "req_agent_machine")
	if err != nil { t.Fatal(err) }
	spec := createAssignmentSpecWithoutReadyAgent(t, state, machineRecord.ID, "req_agent_spec")
	if _, err := state.CreateManagedAgent(ctx, spec.ID, nil, agent.UpdateModeManual, "req_agent_second", "test:operator"); fault.Code(err) != fault.AgentConflict { t.Fatalf("second agent code=%q err=%v", fault.Code(err), err) }
}

func TestQueueAgentBuildRejectsGenericShellCapability(t *testing.T) {
	state := testStore(t)
	ctx := context.Background()
	machineRecord, _, err := state.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:40:03"}, "req_agent_machine")
	if err != nil { t.Fatal(err) }
	spec := createAssignmentSpecWithoutReadyAgent(t, state, machineRecord.ID, "req_agent_spec")
	record, err := state.ManagedAgentByInstallation(ctx, spec.ID)
	if err != nil { t.Fatal(err) }
	if _, err := state.FailAgentBuild(ctx, mustAgentBuild(t, state, record.ID, 1).ID, fault.AgentBuildFailed, "test failure", "req_fail_initial"); err != nil { t.Fatal(err) }
	if _, err := state.QueueAgentBuild(ctx, record.ID, []string{"shell"}, "req_agent_invalid", "test:operator"); fault.Code(err) != fault.AgentInvalid { t.Fatalf("invalid capability code=%q err=%v", fault.Code(err), err) }
}

func TestAgentBuildLifecyclePersistsArtifactMetadataAndAudit(t *testing.T) {
	state := testStore(t)
	ctx := context.Background()
	machineRecord, _, err := state.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:40:04"}, "req_agent_machine")
	if err != nil { t.Fatal(err) }
	spec := createAssignmentSpecWithoutReadyAgent(t, state, machineRecord.ID, "req_agent_spec")
	record, err := state.ManagedAgentByInstallation(ctx, spec.ID)
	if err != nil { t.Fatal(err) }
	claimedRecord, build, err := state.ClaimNextAgentBuild(ctx, "0.2.0-dev.1", "req_build_claim")
	if err != nil { t.Fatal(err) }
	if claimedRecord.ID != record.ID || build.State != agent.BuildStateBuilding || build.Version != "0.2.0-dev.1" { t.Fatalf("unexpected claimed build: record=%+v build=%+v", claimedRecord, build) }
	shaA := strings.Repeat("a", 64)
	shaB := strings.Repeat("b", 64)
	ready, err := state.CompleteAgentBuild(ctx, build.ID, "aegispxe-agent_test_amd64.deb", shaA, 1234, shaB, testManifestSignature, "req_build_ready")
	if err != nil { t.Fatal(err) }
	if ready.State != agent.BuildStateReady || ready.PackageSHA256 != shaA || ready.ManifestSHA256 != shaB || ready.ManifestSignature != testManifestSignature { t.Fatalf("unexpected ready build: %+v", ready) }
	persisted, err := state.AgentBuild(ctx, record.ID, 1)
	if err != nil { t.Fatal(err) }
	if persisted.State != agent.BuildStateReady || persisted.PackagePath != ready.PackagePath || persisted.ManifestSignature != ready.ManifestSignature { t.Fatalf("unexpected persisted build: %+v", persisted) }
	updated, err := state.ManagedAgent(ctx, record.ID)
	if err != nil { t.Fatal(err) }
	if updated.State != agent.StateReady { t.Fatalf("agent state=%q want=%q", updated.State, agent.StateReady) }
	events, err := state.Events(ctx, event.EntityAgent, record.ID)
	if err != nil { t.Fatal(err) }
	if len(events) != 4 || events[2].Type != event.AgentBuildStarted || events[3].Type != event.AgentBuildReady { t.Fatalf("unexpected build events: %+v", events) }
}

func TestCompleteAgentBuildRejectsMalformedSignature(t *testing.T) {
	state := testStore(t)
	ctx := context.Background()
	machineRecord, _, err := state.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:40:05"}, "req_agent_machine")
	if err != nil { t.Fatal(err) }
	createAssignmentSpecWithoutReadyAgent(t, state, machineRecord.ID, "req_agent_spec")
	_, build, err := state.ClaimNextAgentBuild(ctx, "0.2.0-dev.1", "req_build_claim")
	if err != nil { t.Fatal(err) }
	_, err = state.CompleteAgentBuild(ctx, build.ID, "agent.deb", strings.Repeat("a", 64), 12, strings.Repeat("b", 64), "not-a-signature", "req_build_ready")
	if fault.Code(err) != fault.AgentBuildInvalid { t.Fatalf("malformed signature code=%q err=%v", fault.Code(err), err) }
}

func mustAgentBuild(t *testing.T, state *Store, agentID string, generation int) agent.Build {
	t.Helper()
	build, err := state.AgentBuild(context.Background(), agentID, generation)
	if err != nil { t.Fatal(err) }
	return build
}
