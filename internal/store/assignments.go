package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/assignment"
	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
	"github.com/Ostsee-Developer/AegisPXE/internal/lifecycle"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
)

func (s *Store) ArmInstallation(ctx context.Context, machineID, installationID, requestID, actor string) (assignment.Assignment, error) {
	machineID = strings.TrimSpace(machineID)
	installationID = strings.TrimSpace(installationID)
	actor = strings.TrimSpace(actor)
	if strings.TrimSpace(requestID) == "" {
		var err error
		requestID, err = idgen.New("req_")
		if err != nil {
			return assignment.Assignment{}, fault.New(fault.StorageFailure, "could not allocate request identifier", err)
		}
	}
	if machineID == "" || installationID == "" || actor == "" {
		s.logAssignmentRejected(ctx, "arm", requestID, machineID, installationID, actor, fault.InstallationAssignmentInvalid, "missing_required_input")
		return assignment.Assignment{}, fault.New(fault.InstallationAssignmentInvalid, "assignment machine, installation and actor are required", nil)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return assignment.Assignment{}, s.storageError("begin assignment arm", err)
	}
	defer tx.Rollback()

	var policy machine.Policy
	if err := tx.QueryRowContext(ctx, `SELECT policy FROM machines WHERE id=?`, machineID).Scan(&policy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.logAssignmentRejected(ctx, "arm", requestID, machineID, installationID, actor, fault.MachineNotFound, "machine_not_found")
			return assignment.Assignment{}, fault.New(fault.MachineNotFound, "machine not found", err)
		}
		return assignment.Assignment{}, s.storageError("read assignment machine", err)
	}
	if policy != machine.PolicyProvision {
		s.logAssignmentRejected(ctx, "arm", requestID, machineID, installationID, actor, fault.InstallationAssignmentInvalid, "machine_not_operator_approved")
		return assignment.Assignment{}, fault.New(fault.InstallationAssignmentInvalid, "machine is not operator-approved for provisioning", nil)
	}

	var specMachineID string
	if err := tx.QueryRowContext(ctx, `SELECT machine_id FROM installation_specs WHERE id=?`, installationID).Scan(&specMachineID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.logAssignmentRejected(ctx, "arm", requestID, machineID, installationID, actor, fault.InstallationNotFound, "installation_not_found")
			return assignment.Assignment{}, fault.New(fault.InstallationNotFound, "installation not found", err)
		}
		return assignment.Assignment{}, s.storageError("read assignment installation", err)
	}
	if specMachineID != machineID {
		s.logAssignmentRejected(ctx, "arm", requestID, machineID, installationID, actor, fault.InstallationAssignmentInvalid, "installation_machine_mismatch")
		return assignment.Assignment{}, fault.New(fault.InstallationAssignmentInvalid, "installation belongs to a different machine", nil)
	}

	var existingState assignment.State
	err = tx.QueryRowContext(ctx, `SELECT state FROM installation_assignments WHERE installation_id=? LIMIT 1`, installationID).Scan(&existingState)
	if err == nil {
		s.logAssignmentRejected(ctx, "arm", requestID, machineID, installationID, actor, fault.InstallationAssignmentInvalid, "installation_already_assigned")
		return assignment.Assignment{}, fault.New(fault.InstallationAssignmentInvalid, "installation has already been assigned", nil)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return assignment.Assignment{}, s.storageError("check installation assignment history", err)
	}

	var existing string
	err = tx.QueryRowContext(ctx, `SELECT installation_id FROM installation_assignments WHERE machine_id=? AND state='armed' LIMIT 1`, machineID).Scan(&existing)
	if err == nil {
		s.logAssignmentRejected(ctx, "arm", requestID, machineID, installationID, actor, fault.InstallationAssignmentConflict, "machine_already_armed")
		return assignment.Assignment{}, fault.New(fault.InstallationAssignmentConflict, "machine already has an armed installation", nil)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return assignment.Assignment{}, s.storageError("check active assignment", err)
	}

	id, err := idgen.New("a_")
	if err != nil {
		return assignment.Assignment{}, fault.New(fault.StorageFailure, "could not allocate assignment identifier", err)
	}
	now := s.now().UTC()
	item := assignment.Assignment{
		ID:               id,
		MachineID:        machineID,
		InstallationID:   installationID,
		State:            assignment.StateArmed,
		TrustRequirement: assignment.TrustRequirementCryptographic,
		ArmedAt:          now,
		ArmedBy:          actor,
	}
	if err := item.Validate(); err != nil {
		s.logAssignmentRejected(ctx, "arm", requestID, machineID, installationID, actor, fault.InstallationAssignmentInvalid, "assignment_validation_failed")
		return assignment.Assignment{}, fault.New(fault.InstallationAssignmentInvalid, "assignment is invalid", err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO installation_assignments(
		id,machine_id,installation_id,state,trust_requirement,armed_at,armed_by,consumed_at,cancelled_at
	) VALUES(?,?,?,?,?,?,?,?,?)`, item.ID, item.MachineID, item.InstallationID, item.State, item.TrustRequirement,
		item.ArmedAt.Format(time.RFC3339Nano), item.ArmedBy, "", ""); err != nil {
		return assignment.Assignment{}, s.storageError("persist armed assignment", err)
	}
	if err := appendServerLifecycleEventTx(ctx, tx, installationID, lifecycle.StageQueued, "server:queued:"+installationID, "installation queued for next destructive PXE boot", requestID, now); err != nil {
		return assignment.Assignment{}, s.storageError("persist installation queued lifecycle event", err)
	}
	if err := appendEventTx(ctx, tx, event.Event{
		EntityType: event.EntityInstallation,
		EntityID:   installationID,
		Type:       event.InstallationArmed,
		OccurredAt: now,
		RequestID:  requestID,
		Actor:      actor,
		Message:    "installation armed for machine; cryptographic boot trust remains required for secrets",
	}); err != nil {
		return assignment.Assignment{}, s.storageError("persist assignment arm event", err)
	}
	if err := tx.Commit(); err != nil {
		return assignment.Assignment{}, s.storageError("commit assignment arm", err)
	}

	s.logger.InfoContext(ctx, "installation assignment armed",
		"component", "store.assignment",
		"operation", "arm",
		"request_id", requestID,
		"machine_id", machineID,
		"installation_id", installationID,
		"assignment_id", item.ID,
		"trust_requirement", item.TrustRequirement,
		"actor", actor,
		"result", "success",
	)
	return item, nil
}

func (s *Store) CancelAssignment(ctx context.Context, installationID, requestID, actor string) (assignment.Assignment, error) {
	installationID = strings.TrimSpace(installationID)
	actor = strings.TrimSpace(actor)
	if strings.TrimSpace(requestID) == "" {
		var err error
		requestID, err = idgen.New("req_")
		if err != nil {
			return assignment.Assignment{}, fault.New(fault.StorageFailure, "could not allocate request identifier", err)
		}
	}
	if installationID == "" || actor == "" {
		s.logAssignmentRejected(ctx, "cancel", requestID, "", installationID, actor, fault.InstallationAssignmentInvalid, "missing_required_input")
		return assignment.Assignment{}, fault.New(fault.InstallationAssignmentInvalid, "assignment installation and actor are required", nil)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return assignment.Assignment{}, s.storageError("begin assignment cancellation", err)
	}
	defer tx.Rollback()

	item, err := assignmentForInstallationTx(ctx, tx, installationID)
	if err != nil {
		if fault.Code(err) == fault.InstallationAssignmentNotFound {
			s.logAssignmentRejected(ctx, "cancel", requestID, "", installationID, actor, fault.InstallationAssignmentNotFound, "assignment_not_found")
		}
		return assignment.Assignment{}, err
	}
	if item.State != assignment.StateArmed {
		s.logAssignmentRejected(ctx, "cancel", requestID, item.MachineID, installationID, actor, fault.InstallationAssignmentInvalid, "assignment_not_armed")
		return assignment.Assignment{}, fault.New(fault.InstallationAssignmentInvalid, "only an armed assignment can be cancelled", nil)
	}
	now := s.now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE installation_assignments SET state='cancelled',cancelled_at=? WHERE id=? AND state='armed'`, now.Format(time.RFC3339Nano), item.ID); err != nil {
		return assignment.Assignment{}, s.storageError("cancel assignment", err)
	}
	if err := appendEventTx(ctx, tx, event.Event{
		EntityType: event.EntityInstallation,
		EntityID:   installationID,
		Type:       event.InstallationAssignmentCancelled,
		OccurredAt: now,
		RequestID:  requestID,
		Actor:      actor,
		Message:    "installation assignment cancelled",
	}); err != nil {
		return assignment.Assignment{}, s.storageError("persist assignment cancellation event", err)
	}
	if err := tx.Commit(); err != nil {
		return assignment.Assignment{}, s.storageError("commit assignment cancellation", err)
	}
	item.State = assignment.StateCancelled
	item.CancelledAt = now

	s.logger.InfoContext(ctx, "installation assignment cancelled",
		"component", "store.assignment",
		"operation", "cancel",
		"request_id", requestID,
		"machine_id", item.MachineID,
		"installation_id", installationID,
		"assignment_id", item.ID,
		"actor", actor,
		"result", "success",
	)
	return item, nil
}

func (s *Store) AssignmentForInstallation(ctx context.Context, installationID string) (assignment.Assignment, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,machine_id,installation_id,state,trust_requirement,armed_at,armed_by,consumed_at,cancelled_at
		FROM installation_assignments WHERE installation_id=?`, strings.TrimSpace(installationID))
	return scanAssignment(row)
}

func (s *Store) ActiveAssignmentForMachine(ctx context.Context, machineID string) (assignment.Assignment, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,machine_id,installation_id,state,trust_requirement,armed_at,armed_by,consumed_at,cancelled_at
		FROM installation_assignments WHERE machine_id=? AND state='armed' LIMIT 1`, strings.TrimSpace(machineID))
	return scanAssignment(row)
}

func (s *Store) Assignments(ctx context.Context) ([]assignment.Assignment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,machine_id,installation_id,state,trust_requirement,armed_at,armed_by,consumed_at,cancelled_at
		FROM installation_assignments ORDER BY armed_at DESC,id DESC`)
	if err != nil {
		return nil, s.storageError("list assignments", err)
	}
	defer rows.Close()

	var items []assignment.Assignment
	for rows.Next() {
		item, err := scanAssignment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, s.storageError("iterate assignments", err)
	}
	return items, nil
}

func (s *Store) logAssignmentRejected(ctx context.Context, operation, requestID, machineID, installationID, actor, code, cause string) {
	s.logger.WarnContext(ctx, "installation assignment operation rejected",
		"component", "store.assignment",
		"operation", operation,
		"request_id", requestID,
		"machine_id", machineID,
		"installation_id", installationID,
		"actor", actor,
		"result", "rejected",
		"error_code", code,
		"cause", cause,
	)
}

type assignmentScanner interface {
	Scan(...any) error
}

func scanAssignment(scanner assignmentScanner) (assignment.Assignment, error) {
	var item assignment.Assignment
	var armedAt, consumedAt, cancelledAt string
	if err := scanner.Scan(&item.ID, &item.MachineID, &item.InstallationID, &item.State, &item.TrustRequirement, &armedAt, &item.ArmedBy, &consumedAt, &cancelledAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return assignment.Assignment{}, fault.New(fault.InstallationAssignmentNotFound, "installation assignment not found", err)
		}
		return assignment.Assignment{}, fault.New(fault.StorageFailure, "could not read installation assignment", err)
	}
	var err error
	item.ArmedAt, err = time.Parse(time.RFC3339Nano, armedAt)
	if err != nil {
		return assignment.Assignment{}, fault.New(fault.StorageFailure, "could not parse assignment arm time", err)
	}
	if consumedAt != "" {
		item.ConsumedAt, err = time.Parse(time.RFC3339Nano, consumedAt)
		if err != nil {
			return assignment.Assignment{}, fault.New(fault.StorageFailure, "could not parse assignment consumption time", err)
		}
	}
	if cancelledAt != "" {
		item.CancelledAt, err = time.Parse(time.RFC3339Nano, cancelledAt)
		if err != nil {
			return assignment.Assignment{}, fault.New(fault.StorageFailure, "could not parse assignment cancellation time", err)
		}
	}
	if err := item.Validate(); err != nil {
		return assignment.Assignment{}, fault.New(fault.StorageFailure, "stored installation assignment is invalid", err)
	}
	return item, nil
}

func assignmentForInstallationTx(ctx context.Context, tx *sql.Tx, installationID string) (assignment.Assignment, error) {
	row := tx.QueryRowContext(ctx, `SELECT id,machine_id,installation_id,state,trust_requirement,armed_at,armed_by,consumed_at,cancelled_at
		FROM installation_assignments WHERE installation_id=?`, installationID)
	return scanAssignment(row)
}
