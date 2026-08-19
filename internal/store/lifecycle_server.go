package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/lifecycle"
)

func appendServerLifecycleEventTx(ctx context.Context, tx *sql.Tx, installationID string, stage lifecycle.Stage, idempotencyKey, message, requestID string, occurredAt time.Time) error {
	if !lifecycle.SourceAllowed(stage, lifecycle.SourceServer) {
		return fault.New(fault.InstallerTelemetryInvalid, "server is not authorized for lifecycle stage", nil)
	}
	var existingStage lifecycle.Stage
	err := tx.QueryRowContext(ctx, `SELECT stage FROM installation_lifecycle_events WHERE installation_id=? AND idempotency_key=?`, installationID, idempotencyKey).Scan(&existingStage)
	if err == nil {
		if existingStage == stage {
			return nil
		}
		return fault.New(fault.InstallerTelemetryConflict, "server lifecycle idempotency key conflicts with existing stage", nil)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fault.New(fault.StorageFailure, "could not inspect server lifecycle idempotency", err)
	}

	current, err := currentLifecycleStageTx(ctx, tx, installationID)
	if err != nil {
		return err
	}
	if !lifecycle.CanAdvance(current, stage) {
		return fault.New(fault.InstallerTelemetryConflict, "server lifecycle event would regress or skip required state", nil)
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	} else {
		occurredAt = occurredAt.UTC()
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO installation_lifecycle_events(
		installation_id,stage,source,received_at,client_at,request_id,idempotency_key,message,error_code,metadata_json
	) VALUES(?,?,?,?,?,?,?,?,?,?)`, installationID, stage, lifecycle.SourceServer, occurredAt.Format(time.RFC3339Nano), "", requestID,
		idempotencyKey, message, "", "{}")
	if err != nil {
		return fault.New(fault.StorageFailure, "could not persist server lifecycle event", err)
	}
	return nil
}
