package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Ostsee-Developer/AegisPXE/internal/assignment"
	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
)

func (s *Store) DeleteInstallation(ctx context.Context, installationID, requestID, actor string) error {
	installationID = strings.TrimSpace(installationID)
	actor = strings.TrimSpace(actor)
	if installationID == "" || actor == "" {
		return fault.New(fault.InstallationSpecInvalid, "installation and actor are required", nil)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return s.storageError("begin installation deletion", err)
	}
	defer tx.Rollback()

	var machineID string
	if err := tx.QueryRowContext(ctx, `SELECT machine_id FROM installation_specs WHERE id=?`, installationID).Scan(&machineID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fault.New(fault.InstallationNotFound, "installation not found", err)
		}
		return s.storageError("read installation before deletion", err)
	}
	var state assignment.State
	err = tx.QueryRowContext(ctx, `SELECT state FROM installation_assignments WHERE installation_id=?`, installationID).Scan(&state)
	if err == nil && state == assignment.StateArmed {
		return fault.New(fault.InstallationDeleteConflict, "armed installation must be cancelled before deletion", nil)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return s.storageError("read assignment before installation deletion", err)
	}

	for _, statement := range []string{
		`DELETE FROM installation_boot_trust_challenges WHERE installation_id=?`,
		`DELETE FROM installation_log_chunks WHERE installation_id=?`,
		`DELETE FROM installation_lifecycle_events WHERE installation_id=?`,
		`DELETE FROM installation_lifecycle_credentials WHERE installation_id=?`,
		`DELETE FROM installation_assignments WHERE installation_id=?`,
		`DELETE FROM events WHERE entity_type='installation' AND entity_id=?`,
	} {
		if _, err := tx.ExecContext(ctx, statement, installationID); err != nil {
			return s.storageError("delete installation dependent state", err)
		}
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM installation_specs WHERE id=?`, installationID)
	if err != nil {
		return s.storageError("delete installation spec", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return fault.New(fault.StorageFailure, "installation deletion did not affect exactly one row", err)
	}
	if err := appendEventTx(ctx, tx, event.Event{EntityType: event.EntitySystem, EntityID: installationID, Type: event.InstallationDeleted, OccurredAt: s.now().UTC(), RequestID: requestID, Actor: actor, Message: "installation and correlated runtime history deleted"}); err != nil {
		return s.storageError("persist installation deletion audit event", err)
	}
	if err := tx.Commit(); err != nil {
		return s.storageError("commit installation deletion", err)
	}
	s.logger.InfoContext(ctx, "installation deleted", "component", "store.installation", "operation", "delete", "request_id", requestID, "installation_id", installationID, "machine_id", machineID, "actor", actor, "result", "success")
	return nil
}

func (s *Store) DeleteMachine(ctx context.Context, machineID, requestID, actor string) error {
	machineID = strings.TrimSpace(machineID)
	actor = strings.TrimSpace(actor)
	if machineID == "" || actor == "" {
		return fault.New(fault.MachineIdentityInvalid, "machine and actor are required", nil)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return s.storageError("begin machine deletion", err)
	}
	defer tx.Rollback()

	var nickname string
	if err := tx.QueryRowContext(ctx, `SELECT nickname FROM machines WHERE id=?`, machineID).Scan(&nickname); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fault.New(fault.MachineNotFound, "machine not found", err)
		}
		return s.storageError("read machine before deletion", err)
	}
	var installationCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM installation_specs WHERE machine_id=?`, machineID).Scan(&installationCount); err != nil {
		return s.storageError("count machine installations before deletion", err)
	}
	if installationCount != 0 {
		return fault.New(fault.MachineDeleteConflict, "delete the machine installations before deleting the machine", nil)
	}
	var armedCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM installation_assignments WHERE machine_id=? AND state='armed'`, machineID).Scan(&armedCount); err != nil {
		return s.storageError("check armed assignments before machine deletion", err)
	}
	if armedCount != 0 {
		return fault.New(fault.MachineDeleteConflict, "machine still has an armed installation assignment", nil)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM machine_boot_trust_keys WHERE machine_id=?`, machineID); err != nil {
		return s.storageError("delete machine boot trust keys", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE entity_type='machine' AND entity_id=?`, machineID); err != nil {
		return s.storageError("delete machine events", err)
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM machines WHERE id=?`, machineID)
	if err != nil {
		return s.storageError("delete machine", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return fault.New(fault.StorageFailure, "machine deletion did not affect exactly one row", err)
	}
	message := "machine inventory deleted"
	if nickname != "" {
		message = "machine inventory deleted; previous nickname: " + nickname
	}
	if err := appendEventTx(ctx, tx, event.Event{EntityType: event.EntitySystem, EntityID: machineID, Type: event.MachineDeleted, OccurredAt: s.now().UTC(), RequestID: requestID, Actor: actor, Message: message}); err != nil {
		return s.storageError("persist machine deletion audit event", err)
	}
	if err := tx.Commit(); err != nil {
		return s.storageError("commit machine deletion", err)
	}
	s.logger.InfoContext(ctx, "machine deleted", "component", "store.machine", "operation", "delete", "request_id", requestID, "machine_id", machineID, "actor", actor, "nickname_set", nickname != "", "result", "success")
	return nil
}
