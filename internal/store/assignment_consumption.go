package store

import (
	"context"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/assignment"
	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
)

// ConsumeAssignment records that AegisPXE has handed the destructive public
// boot payload to the installer. Consumption is scheduling state only: it does
// not claim INSTALLER_STARTED, SUCCESS, validation, or cryptographic trust.
func (s *Store) ConsumeAssignment(ctx context.Context, installationID, requestID, actor string) (assignment.Assignment, error) {
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
		return assignment.Assignment{}, fault.New(fault.InstallationAssignmentInvalid, "assignment installation and actor are required", nil)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return assignment.Assignment{}, s.storageError("begin assignment consumption", err)
	}
	defer tx.Rollback()

	item, err := assignmentForInstallationTx(ctx, tx, installationID)
	if err != nil {
		return assignment.Assignment{}, err
	}
	if item.State != assignment.StateArmed {
		s.logAssignmentRejected(ctx, "consume_boot_handoff", requestID, item.MachineID, installationID, actor, fault.InstallationAssignmentInvalid, "assignment_not_armed")
		return assignment.Assignment{}, fault.New(fault.InstallationAssignmentInvalid, "only an armed assignment can be consumed", nil)
	}

	now := s.now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE installation_assignments SET state='consumed',consumed_at=? WHERE id=? AND state='armed'`, now.Format(time.RFC3339Nano), item.ID)
	if err != nil {
		return assignment.Assignment{}, s.storageError("consume assignment boot handoff", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return assignment.Assignment{}, s.storageError("read assignment consumption result", err)
	}
	if rows != 1 {
		return assignment.Assignment{}, fault.New(fault.InstallationAssignmentInvalid, "assignment changed while consuming boot handoff", nil)
	}

	if err := appendEventTx(ctx, tx, event.Event{
		EntityType: event.EntityInstallation,
		EntityID:   installationID,
		Type:       event.InstallationAssignmentConsumed,
		OccurredAt: now,
		RequestID:  requestID,
		Actor:      actor,
		Message:    "destructive public boot handoff consumed; installer runtime state remains unverified",
	}); err != nil {
		return assignment.Assignment{}, s.storageError("persist assignment consumption event", err)
	}
	if err := tx.Commit(); err != nil {
		return assignment.Assignment{}, s.storageError("commit assignment consumption", err)
	}

	item.State = assignment.StateConsumed
	item.ConsumedAt = now
	s.logger.InfoContext(ctx, "installation assignment boot handoff consumed",
		"component", "store.assignment",
		"operation", "consume_boot_handoff",
		"request_id", requestID,
		"machine_id", item.MachineID,
		"installation_id", installationID,
		"assignment_id", item.ID,
		"actor", actor,
		"result", "success",
	)
	return item, nil
}
