package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/agent"
	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
	"github.com/Ostsee-Developer/AegisPXE/internal/installation"
	"github.com/Ostsee-Developer/AegisPXE/internal/lifecycle"
)

func (s *Store) CreateInstallationSpec(ctx context.Context, spec installation.Spec, requestID string) (installation.Spec, error) {
	if err := spec.Validate(); err != nil {
		return installation.Spec{}, fault.New(fault.InstallationSpecInvalid, "installation spec is invalid", err)
	}
	if strings.TrimSpace(spec.ID) != "" || !spec.CreatedAt.IsZero() {
		return installation.Spec{}, fault.New(fault.InstallationSpecInvalid, "installation identity and creation time are server assigned", nil)
	}
	if strings.TrimSpace(requestID) == "" {
		var err error
		requestID, err = idgen.New("req_")
		if err != nil {
			return installation.Spec{}, fault.New(fault.StorageFailure, "could not allocate request identifier", err)
		}
	}
	if _, err := s.Machine(ctx, spec.MachineID); err != nil {
		return installation.Spec{}, err
	}
	id, err := idgen.New("i_")
	if err != nil {
		return installation.Spec{}, fault.New(fault.StorageFailure, "could not allocate installation identifier", err)
	}
	spec.ID = id
	spec.CreatedAt = s.now().UTC()

	profileJSON, err := json.Marshal(spec.Profile)
	if err != nil {
		return installation.Spec{}, fault.New(fault.InstallationSpecInvalid, "could not serialize installation profile snapshot", err)
	}
	artifactsJSON, err := json.Marshal(spec.Artifacts)
	if err != nil {
		return installation.Spec{}, fault.New(fault.InstallationSpecInvalid, "could not serialize installation artifacts", err)
	}
	storageJSON, err := json.Marshal(spec.Storage)
	if err != nil {
		return installation.Spec{}, fault.New(fault.InstallationSpecInvalid, "could not serialize installation storage", err)
	}
	securityJSON, err := json.Marshal(spec.Security)
	if err != nil {
		return installation.Spec{}, fault.New(fault.InstallationSpecInvalid, "could not serialize installation security", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return installation.Spec{}, s.storageError("begin installation creation", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `INSERT INTO installation_specs(
		id,machine_id,driver_id,driver_version,os_release,architecture,profile_id,profile_revision,
		profile_json,artifacts_json,storage_json,security_json,lifecycle_credential_id,created_at,created_by
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, spec.ID, spec.MachineID, spec.DriverID, spec.DriverVersion, spec.OSRelease, spec.Architecture, spec.ProfileID, spec.ProfileRevision, string(profileJSON), string(artifactsJSON), string(storageJSON), string(securityJSON), spec.LifecycleCredentialID, spec.CreatedAt.Format(time.RFC3339Nano), spec.CreatedBy)
	if err != nil {
		return installation.Spec{}, s.storageError("persist installation spec", err)
	}
	if err := appendServerLifecycleEventTx(ctx, tx, spec.ID, lifecycle.StageCreated, "server:created:"+spec.ID, "immutable installation spec created", requestID, spec.CreatedAt); err != nil {
		return installation.Spec{}, s.storageError("persist installation lifecycle creation", err)
	}
	if err := appendEventTx(ctx, tx, event.Event{EntityType: event.EntityInstallation, EntityID: spec.ID, Type: event.InstallationCreated, OccurredAt: spec.CreatedAt, RequestID: requestID, Actor: spec.CreatedBy, Message: "immutable installation spec created"}); err != nil {
		return installation.Spec{}, s.storageError("persist installation creation event", err)
	}

	// Every new InstallationSpec owns exactly one managed agent identity and
	// exactly one initial queued build. Keeping this in the same transaction
	// prevents a provisionable installation from existing without its agent.
	agentRecord, agentBuild, err := createManagedAgentTx(ctx, tx, spec.ID, spec.MachineID, spec.Architecture, nil, agent.UpdateModeManual, requestID, spec.CreatedBy, spec.CreatedAt)
	if err != nil {
		if fault.Code(err) != "" {
			return installation.Spec{}, err
		}
		return installation.Spec{}, s.storageError("create installation managed agent", err)
	}

	if err := tx.Commit(); err != nil {
		return installation.Spec{}, s.storageError("commit installation creation", err)
	}

	s.logger.InfoContext(ctx, "installation spec created", "component", "store.installation", "operation", "create", "request_id", requestID, "installation_id", spec.ID, "machine_id", spec.MachineID, "driver_id", spec.DriverID, "driver_version", spec.DriverVersion, "profile_id", spec.ProfileID, "profile_revision", spec.ProfileRevision, "profile_schema_version", spec.Profile.SchemaVersion, "agent_id", agentRecord.ID, "agent_build_id", agentBuild.ID, "agent_build_state", agentBuild.State, "actor", spec.CreatedBy, "result", "success")
	s.logManagedAgentCreated(ctx, agentRecord, agentBuild, requestID, spec.CreatedBy)
	return spec.Clone(), nil
}

func (s *Store) InstallationSpec(ctx context.Context, id string) (installation.Spec, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,machine_id,driver_id,driver_version,os_release,architecture,profile_id,profile_revision,
		profile_json,artifacts_json,storage_json,security_json,lifecycle_credential_id,created_at,created_by
		FROM installation_specs WHERE id=?`, strings.TrimSpace(id))

	var spec installation.Spec
	var profileJSON, artifactsJSON, storageJSON, securityJSON, createdAt string
	if err := row.Scan(&spec.ID, &spec.MachineID, &spec.DriverID, &spec.DriverVersion, &spec.OSRelease, &spec.Architecture, &spec.ProfileID, &spec.ProfileRevision, &profileJSON, &artifactsJSON, &storageJSON, &securityJSON, &spec.LifecycleCredentialID, &createdAt, &spec.CreatedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return installation.Spec{}, fault.New(fault.InstallationNotFound, "installation not found", err)
		}
		return installation.Spec{}, s.storageError("read installation spec", err)
	}
	if err := json.Unmarshal([]byte(profileJSON), &spec.Profile); err != nil {
		return installation.Spec{}, s.storageError("decode installation profile snapshot", err)
	}
	if err := json.Unmarshal([]byte(artifactsJSON), &spec.Artifacts); err != nil {
		return installation.Spec{}, s.storageError("decode installation artifacts", err)
	}
	if err := json.Unmarshal([]byte(storageJSON), &spec.Storage); err != nil {
		return installation.Spec{}, s.storageError("decode installation storage", err)
	}
	if err := json.Unmarshal([]byte(securityJSON), &spec.Security); err != nil {
		return installation.Spec{}, s.storageError("decode installation security", err)
	}
	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return installation.Spec{}, s.storageError("parse installation creation time", err)
	}
	spec.CreatedAt = parsedCreatedAt
	return spec.Clone(), nil
}
