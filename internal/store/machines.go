package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
)

func (s *Store) DiscoverMachine(ctx context.Context, observation machine.Observation, requestID string) (machine.Machine, bool, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		var err error
		requestID, err = idgen.New("req_")
		if err != nil {
			s.logger.ErrorContext(ctx, "request ID allocation failed", "component", "store.machine", "operation", "discover", "error_code", fault.StorageFailure, "error", err)
			return machine.Machine{}, false, fault.New(fault.StorageFailure, "could not allocate request identifier", err)
		}
	}

	architecture := strings.TrimSpace(observation.Architecture)
	firmware := strings.TrimSpace(observation.Firmware)
	if len(architecture) > 64 || len(firmware) > 64 {
		err := fault.New(fault.MachineIdentityInvalid, "machine observation metadata is too large", nil)
		s.logger.WarnContext(ctx, "machine observation rejected", "component", "store.machine", "operation", "discover", "request_id", requestID, "error_code", fault.Code(err))
		return machine.Machine{}, false, err
	}
	identifiers, err := observation.Identifiers()
	if err != nil {
		wrapped := fault.New(fault.MachineIdentityInvalid, "machine identity is invalid", err)
		s.logger.WarnContext(ctx, "machine observation rejected", "component", "store.machine", "operation", "discover", "request_id", requestID, "error_code", fault.Code(wrapped), "cause", err.Error())
		return machine.Machine{}, false, wrapped
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return machine.Machine{}, false, s.storageError("begin machine discovery", err)
	}
	defer tx.Rollback()

	matches := map[string]struct{}{}
	for _, identifier := range identifiers {
		var machineID string
		err := tx.QueryRowContext(ctx, `SELECT machine_id FROM machine_identifiers WHERE kind=? AND value=?`, identifier.Kind, identifier.Value).Scan(&machineID)
		switch {
		case err == nil:
			matches[machineID] = struct{}{}
		case errors.Is(err, sql.ErrNoRows):
			continue
		default:
			return machine.Machine{}, false, s.storageError("resolve machine identity", err)
		}
	}
	if len(matches) > 1 {
		err := fault.New(fault.MachineIdentityConflict, "machine identifiers resolve to different machines", nil)
		s.logger.ErrorContext(ctx, "machine identity conflict", "component", "store.machine", "operation", "discover", "request_id", requestID, "error_code", fault.Code(err))
		return machine.Machine{}, false, err
	}

	now := s.now().UTC()
	stamp := now.Format(time.RFC3339Nano)
	machineID := ""
	created := len(matches) == 0
	for id := range matches {
		machineID = id
	}

	if created {
		machineID, err = idgen.New("m_")
		if err != nil {
			s.logger.ErrorContext(ctx, "machine ID allocation failed", "component", "store.machine", "operation", "discover", "request_id", requestID, "error_code", fault.StorageFailure, "error", err)
			return machine.Machine{}, false, fault.New(fault.StorageFailure, "could not allocate machine identifier", err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO machines(id,nickname,policy,architecture,firmware,first_seen,last_seen) VALUES(?,?,?,?,?,?,?)`, machineID, "", machine.PolicyPending, architecture, firmware, stamp, stamp)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE machines SET architecture=CASE WHEN ?='' THEN architecture ELSE ? END,
			firmware=CASE WHEN ?='' THEN firmware ELSE ? END,last_seen=? WHERE id=?`, architecture, architecture, firmware, firmware, stamp, machineID)
	}
	if err != nil {
		return machine.Machine{}, false, s.storageError("persist machine observation", err)
	}

	for _, identifier := range identifiers {
		if _, err := tx.ExecContext(ctx, `INSERT INTO machine_identifiers(machine_id,kind,value,first_seen,last_seen) VALUES(?,?,?,?,?)
			ON CONFLICT(kind,value) DO UPDATE SET last_seen=excluded.last_seen`, machineID, identifier.Kind, identifier.Value, stamp, stamp); err != nil {
			return machine.Machine{}, false, s.storageError("persist machine identifier", err)
		}
	}

	eventType := event.MachineSeen
	message := "machine observation refreshed"
	if created {
		eventType = event.MachineDiscovered
		message = "machine discovered"
	}
	if err := appendEventTx(ctx, tx, event.Event{EntityType: event.EntityMachine, EntityID: machineID, Type: eventType, OccurredAt: now, RequestID: requestID, Actor: "system:pxe", Message: message}); err != nil {
		return machine.Machine{}, false, s.storageError("persist machine event", err)
	}

	result, err := machineByIDTx(ctx, tx, machineID)
	if err != nil {
		return machine.Machine{}, false, s.storageError("read machine after discovery", err)
	}
	if err := tx.Commit(); err != nil {
		return machine.Machine{}, false, s.storageError("commit machine discovery", err)
	}

	outcome := "seen"
	if created {
		outcome = "created"
	}
	s.logger.InfoContext(ctx, "machine discovery recorded", "component", "store.machine", "operation", "discover", "request_id", requestID, "machine_id", machineID, "policy", result.Policy, "result", outcome)
	return result, created, nil
}

func (s *Store) Machine(ctx context.Context, id string) (machine.Machine, error) {
	result, err := scanMachine(s.db.QueryRowContext(ctx, `SELECT id,nickname,policy,architecture,firmware,first_seen,last_seen FROM machines WHERE id=?`, strings.TrimSpace(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return machine.Machine{}, fault.New(fault.MachineNotFound, "machine not found", err)
	}
	if err != nil {
		return machine.Machine{}, s.storageError("read machine", err)
	}
	return result, nil
}

func (s *Store) SetMachineNickname(ctx context.Context, machineID, nickname, requestID, actor string) (machine.Machine, error) {
	machineID = strings.TrimSpace(machineID)
	actor = strings.TrimSpace(actor)
	if machineID == "" || actor == "" {
		return machine.Machine{}, fault.New(fault.MachineIdentityInvalid, "machine and actor are required", nil)
	}
	nickname, err := machine.NormalizeNickname(nickname)
	if err != nil {
		return machine.Machine{}, fault.New(fault.MachineIdentityInvalid, "machine nickname is invalid", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return machine.Machine{}, s.storageError("begin machine nickname update", err)
	}
	defer tx.Rollback()
	current, err := machineByIDTx(ctx, tx, machineID)
	if errors.Is(err, sql.ErrNoRows) {
		return machine.Machine{}, fault.New(fault.MachineNotFound, "machine not found", err)
	}
	if err != nil {
		return machine.Machine{}, s.storageError("read machine before nickname update", err)
	}
	if current.Nickname == nickname {
		return current, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE machines SET nickname=? WHERE id=?`, nickname, machineID); err != nil {
		return machine.Machine{}, s.storageError("persist machine nickname", err)
	}
	now := s.now().UTC()
	if err := appendEventTx(ctx, tx, event.Event{EntityType: event.EntityMachine, EntityID: machineID, Type: event.MachineNicknameChanged, OccurredAt: now, RequestID: requestID, Actor: actor, Message: "machine nickname changed"}); err != nil {
		return machine.Machine{}, s.storageError("persist machine nickname event", err)
	}
	result, err := machineByIDTx(ctx, tx, machineID)
	if err != nil {
		return machine.Machine{}, s.storageError("read machine after nickname update", err)
	}
	if err := tx.Commit(); err != nil {
		return machine.Machine{}, s.storageError("commit machine nickname update", err)
	}
	s.logger.InfoContext(ctx, "machine nickname changed", "component", "store.machine", "operation", "set_nickname", "request_id", requestID, "machine_id", machineID, "actor", actor, "nickname_set", nickname != "", "result", "success")
	return result, nil
}

func machineByIDTx(ctx context.Context, tx *sql.Tx, id string) (machine.Machine, error) {
	return scanMachine(tx.QueryRowContext(ctx, `SELECT id,nickname,policy,architecture,firmware,first_seen,last_seen FROM machines WHERE id=?`, id))
}

type scanner interface {
	Scan(dest ...any) error
}

func scanMachine(row scanner) (machine.Machine, error) {
	var result machine.Machine
	var firstSeen, lastSeen string
	if err := row.Scan(&result.ID, &result.Nickname, &result.Policy, &result.Architecture, &result.Firmware, &firstSeen, &lastSeen); err != nil {
		return machine.Machine{}, err
	}
	var err error
	result.FirstSeen, err = time.Parse(time.RFC3339Nano, firstSeen)
	if err != nil {
		return machine.Machine{}, fmt.Errorf("parse first_seen: %w", err)
	}
	result.LastSeen, err = time.Parse(time.RFC3339Nano, lastSeen)
	if err != nil {
		return machine.Machine{}, fmt.Errorf("parse last_seen: %w", err)
	}
	return result, nil
}

func (s *Store) storageError(message string, err error) error {
	s.logger.Error("storage operation failed", "component", "store", "operation", message, "error_code", fault.StorageFailure, "error", err)
	return fault.New(fault.StorageFailure, message, err)
}
