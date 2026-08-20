package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/agent"
	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
	"github.com/Ostsee-Developer/AegisPXE/internal/observability"
)

func TestAgentEnrollmentConsumesSingleUseCredentialAndRejectsReplay(t *testing.T) {
	state := testStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	state.now = func() time.Time { return now }
	machineRecord, _, err := state.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:60:01"}, "req_agent_enroll_machine")
	if err != nil {
		t.Fatal(err)
	}
	spec := createAssignmentSpecWithoutReadyAgent(t, state, machineRecord.ID, "req_agent_enroll_spec")
	makeInitialAgentBuildReady(t, state)
	record, err := state.ManagedAgentByInstallation(ctx, spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	credential, secret, err := state.CreateAgentEnrollmentCredential(ctx, record.ID, time.Hour, "req_agent_credential", "system:provisioner")
	if err != nil {
		t.Fatal(err)
	}
	if secret == "" || credential.AgentID != record.ID {
		t.Fatalf("unexpected credential: %+v secret_empty=%v", credential, secret == "")
	}
	var storedVerifier []byte
	if err := state.db.QueryRowContext(ctx, `SELECT secret_sha256 FROM agent_enrollment_credentials WHERE credential_id=?`, credential.ID).Scan(&storedVerifier); err != nil {
		t.Fatal(err)
	}
	wantVerifier := sha256.Sum256([]byte(secret))
	if !bytes.Equal(storedVerifier, wantVerifier[:]) || bytes.Contains(storedVerifier, []byte(secret)) {
		t.Fatal("agent enrollment credential was not persisted as a fixed-size verifier")
	}

	certificate := testAgentCertificate(record.ID, now)
	enrolled, err := state.CompleteAgentEnrollment(ctx, record.ID, secret, certificate, "req_agent_enroll")
	if err != nil {
		t.Fatal(err)
	}
	if enrolled.State != agent.StateOffline {
		t.Fatalf("enrolled state=%q want=%q", enrolled.State, agent.StateOffline)
	}
	if _, err := state.CompleteAgentEnrollment(ctx, record.ID, secret, certificate, "req_agent_replay"); fault.Code(err) != fault.AgentEnrollmentReplay {
		t.Fatalf("replay code=%q err=%v", fault.Code(err), err)
	}
	events, err := state.Events(ctx, event.EntityAgent, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	var completed, certificateIssued bool
	for _, item := range events {
		completed = completed || item.Type == event.AgentEnrollmentCompleted
		certificateIssued = certificateIssued || item.Type == event.AgentCertificateIssued
	}
	if !completed || !certificateIssued {
		t.Fatalf("missing enrollment audit events: %+v", events)
	}
}

func TestAgentEnrollmentRejectsExpiredCredential(t *testing.T) {
	state := testStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	state.now = func() time.Time { return now }
	machineRecord, _, err := state.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:60:02"}, "req_agent_expired_machine")
	if err != nil {
		t.Fatal(err)
	}
	spec := createAssignmentSpecWithoutReadyAgent(t, state, machineRecord.ID, "req_agent_expired_spec")
	makeInitialAgentBuildReady(t, state)
	record, err := state.ManagedAgentByInstallation(ctx, spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, secret, err := state.CreateAgentEnrollmentCredential(ctx, record.ID, time.Minute, "req_agent_expired_credential", "system:provisioner")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := state.CompleteAgentEnrollment(ctx, record.ID, secret, testAgentCertificate(record.ID, now), "req_agent_expired_enroll"); fault.Code(err) != fault.AgentEnrollmentExpired {
		t.Fatalf("expired code=%q err=%v", fault.Code(err), err)
	}
}

func TestAgentHeartbeatAuthenticatesBuildAndProjectsPresence(t *testing.T) {
	state := testStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	state.now = func() time.Time { return now }
	machineRecord, _, err := state.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:60:03"}, "req_agent_heartbeat_machine")
	if err != nil {
		t.Fatal(err)
	}
	spec := createAssignmentSpecWithoutReadyAgent(t, state, machineRecord.ID, "req_agent_heartbeat_spec")
	makeInitialAgentBuildReady(t, state)
	record, err := state.ManagedAgentByInstallation(ctx, spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, secret, err := state.CreateAgentEnrollmentCredential(ctx, record.ID, time.Hour, "req_agent_heartbeat_credential", "system:provisioner")
	if err != nil {
		t.Fatal(err)
	}
	certificate := testAgentCertificate(record.ID, now)
	if _, err := state.CompleteAgentEnrollment(ctx, record.ID, secret, certificate, "req_agent_heartbeat_enroll"); err != nil {
		t.Fatal(err)
	}
	heartbeat := agent.Heartbeat{Version: "0.2.0-dev.1", Generation: 1, BootID: "boot-heartbeat-1", UptimeSeconds: 15, Hostname: "aegis-node", Kernel: "6.12.0", Architecture: "amd64"}
	online, err := state.RecordAgentHeartbeat(ctx, record.ID, certificate.Fingerprint, heartbeat, "req_agent_heartbeat")
	if err != nil {
		t.Fatal(err)
	}
	if online.State != agent.StateOnline || online.ActiveGeneration != 1 || online.ActiveVersion != heartbeat.Version || online.LastSeenAt.IsZero() {
		t.Fatalf("unexpected heartbeat projection: %+v", online)
	}
	now = now.Add(100 * time.Second)
	degraded, err := state.ManagedAgent(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if presence := agent.ProjectPresence(degraded, now); presence != agent.StateDegraded {
		t.Fatalf("presence=%q want=%q", presence, agent.StateDegraded)
	}
	now = now.Add(100 * time.Second)
	offline, err := state.ManagedAgent(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if presence := agent.ProjectPresence(offline, now); presence != agent.StateOffline {
		t.Fatalf("presence=%q want=%q", presence, agent.StateOffline)
	}
	wrong := heartbeat
	wrong.Generation = 2
	if _, err := state.RecordAgentHeartbeat(ctx, record.ID, certificate.Fingerprint, wrong, "req_agent_heartbeat_wrong"); fault.Code(err) != fault.AgentHeartbeatInvalid {
		t.Fatalf("wrong generation code=%q err=%v", fault.Code(err), err)
	}
}

func TestAgentEnrollmentLoggingDoesNotExposeSecret(t *testing.T) {
	var logs bytes.Buffer
	logger := observability.New(&logs, slog.LevelDebug)
	state, err := Open(context.Background(), filepath.Join(t.TempDir(), "aegispxe.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	ctx := context.Background()
	machineRecord, _, err := state.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:60:04"}, "req_agent_log_machine")
	if err != nil {
		t.Fatal(err)
	}
	spec := createAssignmentSpecWithoutReadyAgent(t, state, machineRecord.ID, "req_agent_log_spec")
	makeInitialAgentBuildReady(t, state)
	record, err := state.ManagedAgentByInstallation(ctx, spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, secret, err := state.CreateAgentEnrollmentCredential(ctx, record.ID, time.Hour, "req_agent_log_credential", "system:provisioner")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatal("agent enrollment secret leaked into structured logs")
	}
}

func testAgentCertificate(agentID string, now time.Time) agent.Certificate {
	return agent.Certificate{
		Fingerprint:     strings.Repeat("c", 64),
		AgentID:         agentID,
		Serial:          "1a",
		PublicKeySHA256: strings.Repeat("d", 64),
		IssuedAt:        now,
		ExpiresAt:       now.Add(365 * 24 * time.Hour),
	}
}
