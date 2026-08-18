package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
)

func (s *Store) DiscoverMachine(ctx context.Context, observation machine.Observation, requestID string) (machine.Machine, bool, error) {
	identifiers, err := observation.Identifiers()
	if err != nil {
		return machine.Machine{}, false, fault.New(fault.MachineIdentityInvalid, "machine identity is invalid", err)
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		requestID, err = idgen.New("req_")
		if err != nil {
			return machine.Machine{}, false, fault.New(fault.StorageFailure, "could not allocate request identifier", err)
		}
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
		case err == sql.ErrNoRows:
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
			return machine.Machine{}, false, fault.New(fault.StorageFailure, "could not allocate machine identifier", err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO machines(id,policy,architecture,firmware,first_seen,last_seen) VALUES(?,?,?,?,?,?)`,
			machineID, machine.PolicyPending, strings.TrimSpace(observation.Architecture), strings.TrimSpace(observation.Firmware), stamp, stamp)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE machines SET architecture=CASE WHEN ?='' THEN architecture ELSE ? END,
			firmware=CASE WHEN ?='' THEN firmware ELSE ? END,last_seen=? WHERE id=?`,
			strings.TrimSpace(observation.Architecture), strings.TrimSpace(observation.Architecture), strings.TrimSpace(observation.Firmware), strings.TrimSpace(observation.Firmware), stamp, machineID)
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
	result, err := scanMachine(s.db.QueryRowContext(ctx, `SELECT id,policy,architecture,firmware,first_seen,last_seen FROM machines WHERE id=?`, id))
	if err != nil {
		return machine.Machine{}, err
	}
	return result, nil
}

func machineByIDTx(ctx context.Context, tx *sql.Tx, id string) (machine.Machine, error) {
	return scanMachine(tx.QueryRowContext(ctx, `SELECT id,policy,architecture,firmware,first_seen,last_seen FROM machines WHERE id=?`, id))
}

type scanner interface {
	Scan(dest ...any) error
}

func scanMachine(row scanner) (machine.Machine, error) {
	var result machine.Machine
	var firstSeen, lastSeen string
	if err := row.Scan(&result.ID, &result.Policy, &result.Architecture, &result.Firmware, &firstSeen, &lastSeen); err != nil {
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
