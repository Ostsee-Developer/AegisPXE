package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/agent"
	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
)

func (s *Store) CreateManagedAgent(ctx context.Context, installationID string, capabilityCeiling []string, updateMode agent.UpdateMode, requestID, actor string) (agent.Record, error) {
	installationID = strings.TrimSpace(installationID)
	requestID = strings.TrimSpace(requestID)
	actor = strings.TrimSpace(actor)
	if installationID == "" || actor == "" {
		return agent.Record{}, fault.New(fault.AgentInvalid, "installation and actor are required", nil)
	}
	if updateMode == "" {
		updateMode = agent.UpdateModeManual
	}
	if !updateMode.Valid() {
		return agent.Record{}, fault.New(fault.AgentInvalid, "agent update mode is invalid", nil)
	}
	capabilities, err := agent.NormalizeCapabilityCeiling(capabilityCeiling)
	if err != nil {
		return agent.Record{}, fault.New(fault.AgentInvalid, "agent capability ceiling is invalid", err)
	}
	if requestID == "" {
		requestID, err = idgen.New("req_")
		if err != nil {
			return agent.Record{}, fault.New(fault.StorageFailure, "could not allocate request identifier", err)
		}
	}
	agentID, err := idgen.NewUUID()
	if err != nil {
		return agent.Record{}, fault.New(fault.StorageFailure, "could not allocate agent identifier", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return agent.Record{}, s.storageError("begin managed agent creation", err)
	}
	defer tx.Rollback()

	var machineID string
	if err := tx.QueryRowContext(ctx, `SELECT machine_id FROM installation_specs WHERE id=?`, installationID).Scan(&machineID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return agent.Record{}, fault.New(fault.InstallationNotFound, "installation not found", err)
		}
		return agent.Record{}, s.storageError("resolve managed agent installation", err)
	}
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT id FROM agents WHERE installation_id=?`, installationID).Scan(&existing)
	switch {
	case err == nil:
		return agent.Record{}, fault.New(fault.AgentConflict, "installation already has a managed agent", nil)
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return agent.Record{}, s.storageError("check managed agent binding", err)
	}

	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return agent.Record{}, fault.New(fault.AgentInvalid, "could not serialize agent capability ceiling", err)
	}
	now := s.now().UTC()
	stamp := now.Format(time.RFC3339Nano)
	record := agent.Record{
		ID:                agentID,
		InstallationID:    installationID,
		MachineID:         machineID,
		State:             agent.StatePendingBuild,
		UpdateMode:        updateMode,
		UpdateState:       agent.UpdateStateIdle,
		CapabilityCeiling: capabilities,
		ActiveGeneration:  0,
		DesiredGeneration: 1,
		LastHeartbeatJSON: "{}",
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := record.Validate(); err != nil {
		return agent.Record{}, fault.New(fault.AgentInvalid, "managed agent record is invalid", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO agents(
		id,installation_id,machine_id,state,update_mode,update_state,capability_ceiling_json,
		active_generation,desired_generation,active_version,last_seen_at,last_heartbeat_json,created_at,updated_at,revoked_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		record.ID, record.InstallationID, record.MachineID, record.State, record.UpdateMode, record.UpdateState, string(capabilitiesJSON),
		record.ActiveGeneration, record.DesiredGeneration, record.ActiveVersion, "", record.LastHeartbeatJSON, stamp, stamp, "",
	); err != nil {
		return agent.Record{}, s.storageError("persist managed agent", err)
	}
	if err := appendEventTx(ctx, tx, event.Event{
		EntityType: event.EntityAgent,
		EntityID:   record.ID,
		Type:       event.AgentCreated,
		OccurredAt: now,
		RequestID:  requestID,
		Actor:      actor,
		Message:    "managed agent identity created",
	}); err != nil {
		return agent.Record{}, s.storageError("persist managed agent creation event", err)
	}
	if err := tx.Commit(); err != nil {
		return agent.Record{}, s.storageError("commit managed agent creation", err)
	}

	s.logger.InfoContext(ctx, "managed agent identity created",
		"component", "store.agent",
		"operation", "create",
		"request_id", requestID,
		"machine_id", record.MachineID,
		"installation_id", record.InstallationID,
		"agent_id", record.ID,
		"agent_state", record.State,
		"update_mode", record.UpdateMode,
		"capability_count", len(record.CapabilityCeiling),
		"result", "success",
	)
	return record.Clone(), nil
}

func (s *Store) ManagedAgent(ctx context.Context, id string) (agent.Record, error) {
	id = strings.TrimSpace(id)
	if err := agent.ValidateID(id); err != nil {
		return agent.Record{}, fault.New(fault.AgentInvalid, "agent identifier is invalid", err)
	}
	record, err := scanManagedAgent(s.db.QueryRowContext(ctx, managedAgentSelect+` WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return agent.Record{}, fault.New(fault.AgentNotFound, "managed agent not found", err)
	}
	if err != nil {
		return agent.Record{}, s.storageError("read managed agent", err)
	}
	return record.Clone(), nil
}

func (s *Store) ManagedAgentByInstallation(ctx context.Context, installationID string) (agent.Record, error) {
	installationID = strings.TrimSpace(installationID)
	if installationID == "" {
		return agent.Record{}, fault.New(fault.AgentInvalid, "installation identifier is required", nil)
	}
	record, err := scanManagedAgent(s.db.QueryRowContext(ctx, managedAgentSelect+` WHERE installation_id=?`, installationID))
	if errors.Is(err, sql.ErrNoRows) {
		return agent.Record{}, fault.New(fault.AgentNotFound, "managed agent not found", err)
	}
	if err != nil {
		return agent.Record{}, s.storageError("read managed agent by installation", err)
	}
	return record.Clone(), nil
}

const managedAgentSelect = `SELECT id,installation_id,machine_id,state,update_mode,update_state,capability_ceiling_json,
	active_generation,desired_generation,active_version,last_seen_at,last_heartbeat_json,created_at,updated_at,revoked_at FROM agents`

func scanManagedAgent(row scanner) (agent.Record, error) {
	var record agent.Record
	var capabilityJSON, lastSeenAt, createdAt, updatedAt, revokedAt string
	if err := row.Scan(
		&record.ID, &record.InstallationID, &record.MachineID, &record.State, &record.UpdateMode, &record.UpdateState, &capabilityJSON,
		&record.ActiveGeneration, &record.DesiredGeneration, &record.ActiveVersion, &lastSeenAt, &record.LastHeartbeatJSON, &createdAt, &updatedAt, &revokedAt,
	); err != nil {
		return agent.Record{}, err
	}
	if err := json.Unmarshal([]byte(capabilityJSON), &record.CapabilityCeiling); err != nil {
		return agent.Record{}, fmt.Errorf("decode agent capability ceiling: %w", err)
	}
	var err error
	if record.LastSeenAt, err = parseOptionalAgentTime(lastSeenAt); err != nil {
		return agent.Record{}, fmt.Errorf("parse agent last seen time: %w", err)
	}
	if record.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return agent.Record{}, fmt.Errorf("parse agent creation time: %w", err)
	}
	if record.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return agent.Record{}, fmt.Errorf("parse agent update time: %w", err)
	}
	if record.RevokedAt, err = parseOptionalAgentTime(revokedAt); err != nil {
		return agent.Record{}, fmt.Errorf("parse agent revocation time: %w", err)
	}
	if err := record.Validate(); err != nil {
		return agent.Record{}, fmt.Errorf("validate persisted managed agent: %w", err)
	}
	return record, nil
}

func parseOptionalAgentTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, value)
}
