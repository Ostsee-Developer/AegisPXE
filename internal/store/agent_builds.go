package store

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/agent"
	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
)

func insertAgentBuildTx(ctx context.Context, tx *sql.Tx, record agent.Record, generation int, architecture string, capabilities []string, now time.Time, requestID, actor string) (agent.Build, error) {
	if generation < 1 {
		return agent.Build{}, fault.New(fault.AgentInvalid, "agent build generation is invalid", nil)
	}
	architecture = strings.TrimSpace(architecture)
	if architecture != "amd64" && architecture != "arm64" {
		return agent.Build{}, fault.New(fault.AgentInvalid, "agent build architecture is unsupported", nil)
	}
	normalized, err := agent.NormalizeCapabilityCeiling(capabilities)
	if err != nil {
		return agent.Build{}, fault.New(fault.AgentInvalid, "agent build capability ceiling is invalid", err)
	}
	buildID, err := idgen.New("ab_")
	if err != nil {
		return agent.Build{}, fault.New(fault.StorageFailure, "could not allocate agent build identifier", err)
	}
	capabilitiesJSON, err := json.Marshal(normalized)
	if err != nil {
		return agent.Build{}, fault.New(fault.AgentInvalid, "could not serialize agent build capabilities", err)
	}
	build := agent.Build{ID: buildID, AgentID: record.ID, Generation: generation, Architecture: architecture, CapabilityCeiling: normalized, State: agent.BuildStateQueued, CreatedAt: now.UTC()}
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_builds(
		id,agent_id,generation,version,architecture,capability_ceiling_json,state,
		package_path,package_sha256,package_size,manifest_sha256,manifest_signature,
		created_at,started_at,ready_at,failed_at,superseded_at,revoked_at,error_code,error_message
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		build.ID, build.AgentID, build.Generation, "", build.Architecture, string(capabilitiesJSON), build.State,
		"", "", 0, "", []byte{}, build.CreatedAt.Format(time.RFC3339Nano), "", "", "", "", "", "", ""); err != nil {
		return agent.Build{}, fmt.Errorf("persist queued agent build: %w", err)
	}
	if err := appendEventTx(ctx, tx, event.Event{EntityType: event.EntityAgent, EntityID: record.ID, Type: event.AgentBuildQueued, OccurredAt: build.CreatedAt, RequestID: requestID, Actor: actor, Message: fmt.Sprintf("managed agent build generation %d queued", generation)}); err != nil {
		return agent.Build{}, fmt.Errorf("persist queued agent build event: %w", err)
	}
	return build.Clone(), nil
}

func (s *Store) QueueAgentBuild(ctx context.Context, agentID string, capabilityCeiling []string, requestID, actor string) (agent.Build, error) {
	agentID = strings.TrimSpace(agentID)
	actor = strings.TrimSpace(actor)
	requestID = strings.TrimSpace(requestID)
	if err := agent.ValidateID(agentID); err != nil || actor == "" {
		return agent.Build{}, fault.New(fault.AgentInvalid, "agent and actor are required", err)
	}
	if requestID == "" {
		var err error
		requestID, err = idgen.New("req_")
		if err != nil {
			return agent.Build{}, fault.New(fault.StorageFailure, "could not allocate request identifier", err)
		}
	}
	normalized, err := agent.NormalizeCapabilityCeiling(capabilityCeiling)
	if err != nil {
		return agent.Build{}, fault.New(fault.AgentInvalid, "agent build capability ceiling is invalid", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return agent.Build{}, s.storageError("begin agent build queue", err)
	}
	defer tx.Rollback()
	record, err := managedAgentByIDTx(ctx, tx, agentID)
	if errors.Is(err, sql.ErrNoRows) {
		return agent.Build{}, fault.New(fault.AgentNotFound, "managed agent not found", err)
	}
	if err != nil {
		return agent.Build{}, s.storageError("read managed agent before build queue", err)
	}
	if record.State == agent.StateRevoked {
		return agent.Build{}, fault.New(fault.AgentConflict, "revoked agent cannot be rebuilt", nil)
	}
	var activeBuilds int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM agent_builds WHERE agent_id=? AND state IN ('queued','building')`, agentID).Scan(&activeBuilds); err != nil {
		return agent.Build{}, s.storageError("check active agent builds", err)
	}
	if activeBuilds != 0 {
		return agent.Build{}, fault.New(fault.AgentConflict, "agent already has a queued or active build", nil)
	}
	generation := record.DesiredGeneration + 1
	now := s.now().UTC()
	architecture, err := buildArchitectureForRecord(ctx, tx, record)
	if err != nil {
		return agent.Build{}, s.storageError("resolve agent build architecture", err)
	}
	build, err := insertAgentBuildTx(ctx, tx, record, generation, architecture, normalized, now, requestID, actor)
	if err != nil {
		if fault.Code(err) != "" {
			return agent.Build{}, err
		}
		return agent.Build{}, s.storageError("queue agent build", err)
	}
	capabilitiesJSON, err := json.Marshal(normalized)
	if err != nil {
		return agent.Build{}, fault.New(fault.AgentInvalid, "could not serialize agent capabilities", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agents SET state=?,desired_generation=?,capability_ceiling_json=?,updated_at=? WHERE id=?`, agent.StatePendingBuild, generation, string(capabilitiesJSON), now.Format(time.RFC3339Nano), agentID); err != nil {
		return agent.Build{}, s.storageError("advance desired agent generation", err)
	}
	if err := tx.Commit(); err != nil {
		return agent.Build{}, s.storageError("commit agent build queue", err)
	}
	s.logger.InfoContext(ctx, "managed agent build queued", "component", "store.agent_build", "operation", "queue", "request_id", requestID, "agent_id", agentID, "installation_id", record.InstallationID, "machine_id", record.MachineID, "generation", generation, "architecture", build.Architecture, "capability_count", len(normalized), "actor", actor, "result", "success")
	return build.Clone(), nil
}

func buildArchitectureForRecord(ctx context.Context, tx *sql.Tx, record agent.Record) (string, error) {
	var architecture string
	if err := tx.QueryRowContext(ctx, `SELECT architecture FROM installation_specs WHERE id=?`, record.InstallationID).Scan(&architecture); err != nil {
		return "", err
	}
	return strings.TrimSpace(architecture), nil
}

func (s *Store) ClaimNextAgentBuild(ctx context.Context, version, requestID string) (agent.Record, agent.Build, error) {
	version = strings.TrimSpace(version)
	requestID = strings.TrimSpace(requestID)
	if version == "" || len(version) > 128 {
		return agent.Record{}, agent.Build{}, fault.New(fault.AgentInvalid, "agent build version is invalid", nil)
	}
	if requestID == "" {
		var err error
		requestID, err = idgen.New("req_")
		if err != nil {
			return agent.Record{}, agent.Build{}, fault.New(fault.StorageFailure, "could not allocate request identifier", err)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return agent.Record{}, agent.Build{}, s.storageError("begin agent build claim", err)
	}
	defer tx.Rollback()
	build, err := scanAgentBuild(tx.QueryRowContext(ctx, agentBuildSelect+` WHERE state='queued' ORDER BY created_at,id LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return agent.Record{}, agent.Build{}, fault.New(fault.AgentBuildQueueEmpty, "no managed agent build is queued", err)
	}
	if err != nil {
		return agent.Record{}, agent.Build{}, s.storageError("read queued agent build", err)
	}
	record, err := managedAgentByIDTx(ctx, tx, build.AgentID)
	if err != nil {
		return agent.Record{}, agent.Build{}, s.storageError("read managed agent for build", err)
	}
	if build.Generation != record.DesiredGeneration || record.State == agent.StateRevoked {
		return agent.Record{}, agent.Build{}, fault.New(fault.AgentConflict, "queued agent build is no longer desired", nil)
	}
	now := s.now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE agent_builds SET state='building',version=?,started_at=? WHERE id=? AND state='queued'`, version, now.Format(time.RFC3339Nano), build.ID)
	if err != nil {
		return agent.Record{}, agent.Build{}, s.storageError("claim queued agent build", err)
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return agent.Record{}, agent.Build{}, fault.New(fault.AgentConflict, "queued agent build was claimed concurrently", err)
	}
	if err := appendEventTx(ctx, tx, event.Event{EntityType: event.EntityAgent, EntityID: record.ID, Type: event.AgentBuildStarted, OccurredAt: now, RequestID: requestID, Actor: "system:agent-builder", Message: fmt.Sprintf("managed agent build generation %d started", build.Generation)}); err != nil {
		return agent.Record{}, agent.Build{}, s.storageError("persist agent build start event", err)
	}
	if err := tx.Commit(); err != nil {
		return agent.Record{}, agent.Build{}, s.storageError("commit agent build claim", err)
	}
	build.State = agent.BuildStateBuilding
	build.Version = version
	build.StartedAt = now
	s.logger.InfoContext(ctx, "managed agent build claimed", "component", "store.agent_build", "operation", "claim", "request_id", requestID, "agent_id", record.ID, "installation_id", record.InstallationID, "machine_id", record.MachineID, "build_id", build.ID, "generation", build.Generation, "version", version, "result", "success")
	return record.Clone(), build.Clone(), nil
}

func (s *Store) CompleteAgentBuild(ctx context.Context, buildID, packagePath, packageSHA256 string, packageSize int64, manifestSHA256, manifestSignature, requestID string) (agent.Build, error) {
	buildID = strings.TrimSpace(buildID)
	packagePath = strings.TrimSpace(packagePath)
	packageSHA256 = strings.TrimSpace(packageSHA256)
	manifestSHA256 = strings.TrimSpace(manifestSHA256)
	manifestSignature = strings.TrimSpace(manifestSignature)
	requestID = strings.TrimSpace(requestID)
	if buildID == "" || packagePath == "" || filepath.Base(packagePath) != packagePath || packageSize <= 0 || !validSHA256Hex(packageSHA256) || !validSHA256Hex(manifestSHA256) || !validManifestSignature(manifestSignature) {
		return agent.Build{}, fault.New(fault.AgentBuildInvalid, "agent build artifact metadata is invalid", nil)
	}
	if requestID == "" {
		var err error
		requestID, err = idgen.New("req_")
		if err != nil {
			return agent.Build{}, fault.New(fault.StorageFailure, "could not allocate request identifier", err)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return agent.Build{}, s.storageError("begin agent build completion", err)
	}
	defer tx.Rollback()
	build, err := scanAgentBuild(tx.QueryRowContext(ctx, agentBuildSelect+` WHERE id=?`, buildID))
	if errors.Is(err, sql.ErrNoRows) {
		return agent.Build{}, fault.New(fault.AgentBuildNotFound, "managed agent build not found", err)
	}
	if err != nil {
		return agent.Build{}, s.storageError("read agent build before completion", err)
	}
	if build.State != agent.BuildStateBuilding {
		return agent.Build{}, fault.New(fault.AgentConflict, "only a building agent package can complete", nil)
	}
	record, err := managedAgentByIDTx(ctx, tx, build.AgentID)
	if err != nil {
		return agent.Build{}, s.storageError("read managed agent before build completion", err)
	}
	if build.Generation != record.DesiredGeneration || record.State == agent.StateRevoked {
		return agent.Build{}, fault.New(fault.AgentConflict, "agent build is no longer desired", nil)
	}
	now := s.now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE agent_builds SET state='ready',package_path=?,package_sha256=?,package_size=?,manifest_sha256=?,manifest_signature=?,ready_at=?,error_code='',error_message='' WHERE id=? AND state='building'`, packagePath, packageSHA256, packageSize, manifestSHA256, []byte(manifestSignature), now.Format(time.RFC3339Nano), buildID); err != nil {
		return agent.Build{}, s.storageError("persist completed agent build", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agents SET state=?,updated_at=? WHERE id=?`, agent.StateReady, now.Format(time.RFC3339Nano), record.ID); err != nil {
		return agent.Build{}, s.storageError("mark managed agent build ready", err)
	}
	if err := appendEventTx(ctx, tx, event.Event{EntityType: event.EntityAgent, EntityID: record.ID, Type: event.AgentBuildReady, OccurredAt: now, RequestID: requestID, Actor: "system:agent-builder", Message: fmt.Sprintf("managed agent build generation %d ready", build.Generation)}); err != nil {
		return agent.Build{}, s.storageError("persist agent build ready event", err)
	}
	if err := tx.Commit(); err != nil {
		return agent.Build{}, s.storageError("commit agent build completion", err)
	}
	build.State = agent.BuildStateReady
	build.PackagePath = packagePath
	build.PackageSHA256 = packageSHA256
	build.PackageSize = packageSize
	build.ManifestSHA256 = manifestSHA256
	build.ManifestSignature = manifestSignature
	build.ReadyAt = now
	s.logger.InfoContext(ctx, "managed agent build ready", "component", "store.agent_build", "operation", "complete", "request_id", requestID, "agent_id", record.ID, "installation_id", record.InstallationID, "machine_id", record.MachineID, "build_id", build.ID, "generation", build.Generation, "package_sha256", packageSHA256, "manifest_sha256", manifestSHA256, "package_size", packageSize, "result", "success")
	return build.Clone(), nil
}

func (s *Store) FailAgentBuild(ctx context.Context, buildID, errorCode, errorMessage, requestID string) (agent.Build, error) {
	buildID = strings.TrimSpace(buildID)
	errorCode = strings.TrimSpace(errorCode)
	errorMessage = strings.TrimSpace(errorMessage)
	requestID = strings.TrimSpace(requestID)
	if buildID == "" || errorCode == "" || len(errorCode) > 96 || errorMessage == "" || len(errorMessage) > 512 {
		return agent.Build{}, fault.New(fault.AgentBuildInvalid, "agent build failure metadata is invalid", nil)
	}
	if requestID == "" {
		var err error
		requestID, err = idgen.New("req_")
		if err != nil {
			return agent.Build{}, fault.New(fault.StorageFailure, "could not allocate request identifier", err)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return agent.Build{}, s.storageError("begin agent build failure", err)
	}
	defer tx.Rollback()
	build, err := scanAgentBuild(tx.QueryRowContext(ctx, agentBuildSelect+` WHERE id=?`, buildID))
	if errors.Is(err, sql.ErrNoRows) {
		return agent.Build{}, fault.New(fault.AgentBuildNotFound, "managed agent build not found", err)
	}
	if err != nil {
		return agent.Build{}, s.storageError("read agent build before failure", err)
	}
	if build.State != agent.BuildStateBuilding && build.State != agent.BuildStateQueued {
		return agent.Build{}, fault.New(fault.AgentConflict, "agent build is not active", nil)
	}
	record, err := managedAgentByIDTx(ctx, tx, build.AgentID)
	if err != nil {
		return agent.Build{}, s.storageError("read managed agent before build failure", err)
	}
	now := s.now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE agent_builds SET state='failed',failed_at=?,error_code=?,error_message=? WHERE id=? AND state IN ('queued','building')`, now.Format(time.RFC3339Nano), errorCode, errorMessage, buildID); err != nil {
		return agent.Build{}, s.storageError("persist failed agent build", err)
	}
	if build.Generation == record.DesiredGeneration && record.State != agent.StateRevoked {
		if _, err := tx.ExecContext(ctx, `UPDATE agents SET state=?,updated_at=? WHERE id=?`, agent.StatePendingBuild, now.Format(time.RFC3339Nano), record.ID); err != nil {
			return agent.Build{}, s.storageError("persist failed managed agent state", err)
		}
	}
	if err := appendEventTx(ctx, tx, event.Event{EntityType: event.EntityAgent, EntityID: record.ID, Type: event.AgentBuildFailed, OccurredAt: now, RequestID: requestID, Actor: "system:agent-builder", Message: fmt.Sprintf("managed agent build generation %d failed", build.Generation), ErrorCode: errorCode}); err != nil {
		return agent.Build{}, s.storageError("persist agent build failure event", err)
	}
	if err := tx.Commit(); err != nil {
		return agent.Build{}, s.storageError("commit agent build failure", err)
	}
	build.State = agent.BuildStateFailed
	build.FailedAt = now
	build.ErrorCode = errorCode
	build.ErrorMessage = errorMessage
	s.logger.ErrorContext(ctx, "managed agent build failed", "component", "store.agent_build", "operation", "fail", "request_id", requestID, "agent_id", record.ID, "installation_id", record.InstallationID, "machine_id", record.MachineID, "build_id", build.ID, "generation", build.Generation, "error_code", errorCode, "result", "failure")
	return build.Clone(), nil
}

func (s *Store) AgentBuild(ctx context.Context, agentID string, generation int) (agent.Build, error) {
	agentID = strings.TrimSpace(agentID)
	if err := agent.ValidateID(agentID); err != nil || generation < 1 {
		return agent.Build{}, fault.New(fault.AgentBuildInvalid, "agent build identity is invalid", err)
	}
	build, err := scanAgentBuild(s.db.QueryRowContext(ctx, agentBuildSelect+` WHERE agent_id=? AND generation=?`, agentID, generation))
	if errors.Is(err, sql.ErrNoRows) {
		return agent.Build{}, fault.New(fault.AgentBuildNotFound, "managed agent build not found", err)
	}
	if err != nil {
		return agent.Build{}, s.storageError("read managed agent build", err)
	}
	return build.Clone(), nil
}

const agentBuildSelect = `SELECT id,agent_id,generation,version,architecture,capability_ceiling_json,state,
	package_path,package_sha256,package_size,manifest_sha256,manifest_signature,
	created_at,started_at,ready_at,failed_at,superseded_at,revoked_at,error_code,error_message FROM agent_builds`

func scanAgentBuild(row scanner) (agent.Build, error) {
	var build agent.Build
	var capabilitiesJSON string
	var signature []byte
	var createdAt, startedAt, readyAt, failedAt, supersededAt, revokedAt string
	if err := row.Scan(&build.ID, &build.AgentID, &build.Generation, &build.Version, &build.Architecture, &capabilitiesJSON, &build.State, &build.PackagePath, &build.PackageSHA256, &build.PackageSize, &build.ManifestSHA256, &signature, &createdAt, &startedAt, &readyAt, &failedAt, &supersededAt, &revokedAt, &build.ErrorCode, &build.ErrorMessage); err != nil {
		return agent.Build{}, err
	}
	if err := json.Unmarshal([]byte(capabilitiesJSON), &build.CapabilityCeiling); err != nil {
		return agent.Build{}, fmt.Errorf("decode agent build capability ceiling: %w", err)
	}
	build.ManifestSignature = string(signature)
	var err error
	if build.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return agent.Build{}, fmt.Errorf("parse agent build creation time: %w", err)
	}
	for _, item := range []struct { value string; target *time.Time }{{startedAt, &build.StartedAt}, {readyAt, &build.ReadyAt}, {failedAt, &build.FailedAt}, {supersededAt, &build.SupersededAt}, {revokedAt, &build.RevokedAt}} {
		if item.value == "" {
			continue
		}
		parsed, parseErr := time.Parse(time.RFC3339Nano, item.value)
		if parseErr != nil {
			return agent.Build{}, fmt.Errorf("parse agent build timestamp: %w", parseErr)
		}
		*item.target = parsed
	}
	return build, nil
}

func managedAgentByIDTx(ctx context.Context, tx *sql.Tx, id string) (agent.Record, error) {
	return scanManagedAgent(tx.QueryRowContext(ctx, managedAgentSelect+` WHERE id=?`, id))
}

func validSHA256Hex(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validManifestSignature(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	return err == nil && len(decoded) == ed25519.SignatureSize
}
