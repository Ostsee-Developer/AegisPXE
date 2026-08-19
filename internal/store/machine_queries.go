package store

import (
	"context"

	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
)

func (s *Store) Machines(ctx context.Context) ([]machine.Machine, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,nickname,policy,architecture,firmware,secure_boot_state,secure_boot_observed_at,first_seen,last_seen FROM machines ORDER BY last_seen DESC, id`)
	if err != nil {
		return nil, s.storageError("query machines", err)
	}
	defer rows.Close()

	var out []machine.Machine
	for rows.Next() {
		item, err := scanMachine(rows)
		if err != nil {
			return nil, s.storageError("scan machine", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, s.storageError("iterate machines", err)
	}
	return out, nil
}

func (s *Store) MachineIdentifiers(ctx context.Context, machineID string) ([]machine.Identifier, error) {
	if _, err := s.Machine(ctx, machineID); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT kind,value FROM machine_identifiers WHERE machine_id=? ORDER BY kind,value`, machineID)
	if err != nil {
		return nil, s.storageError("query machine identifiers", err)
	}
	defer rows.Close()

	var out []machine.Identifier
	for rows.Next() {
		var item machine.Identifier
		if err := rows.Scan(&item.Kind, &item.Value); err != nil {
			return nil, s.storageError("scan machine identifier", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, s.storageError("iterate machine identifiers", err)
	}
	return out, nil
}
