package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
)

func (s *Store) SetMachinePolicy(ctx context.Context, machineID string, policy machine.Policy, requestID, actor string) (machine.Machine, error) {
	if !validPolicy(policy) {
		return machine.Machine{}, fault.New(fault.MachinePolicyInvalid, "machine policy is invalid", nil)
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return machine.Machine{}, fault.New(fault.MachinePolicyInvalid, "machine policy actor is required", nil)
	}
	if strings.TrimSpace(requestID) == "" {
		var err error
		requestID, err = idgen.New("req_")
		if err != nil {
			return machine.Machine{}, fault.New(fault.StorageFailure, "could not allocate request identifier", err)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return machine.Machine{}, s.storageError("begin machine policy change", err)
	}
	defer tx.Rollback()

	current, err := machineByIDTx(ctx, tx, machineID)
	if err == sql.ErrNoRows {
		return machine.Machine{}, fault.New(fault.MachineNotFound, "machine not found", err)
	}
	if err != nil {
		return machine.Machine{}, s.storageError("read machine policy", err)
	}
	if current.Policy == policy {
		return current, nil
	}

	if _, err := tx.ExecContext(ctx, `UPDATE machines SET policy=? WHERE id=?`, policy, machineID); err != nil {
		return machine.Machine{}, s.storageError("update machine policy", err)
	}
	now := s.now().UTC()
	if err := appendEventTx(ctx, tx, event.Event{
		EntityType: event.EntityMachine,
		EntityID:   machineID,
		Type:       event.MachinePolicyChanged,
		OccurredAt: now,
		RequestID:  requestID,
		Actor:      actor,
		Message:    fmt.Sprintf("machine policy changed from %s to %s", current.Policy, policy),
	}); err != nil {
		return machine.Machine{}, s.storageError("persist machine policy event", err)
	}
	if err := tx.Commit(); err != nil {
		return machine.Machine{}, s.storageError("commit machine policy change", err)
	}

	current.Policy = policy
	s.logger.InfoContext(ctx, "machine policy changed", "component", "store.machine", "operation", "set_policy", "request_id", requestID, "machine_id", machineID, "actor", actor, "policy", policy)
	return current, nil
}

func validPolicy(policy machine.Policy) bool {
	switch policy {
	case machine.PolicyPending, machine.PolicyLocal, machine.PolicyProvision, machine.PolicyBlocked:
		return true
	default:
		return false
	}
}
