package store

import (
	"context"
	"strings"

	"github.com/Ostsee-Developer/AegisPXE/internal/installation"
)

func (s *Store) InstallationSpecs(ctx context.Context) ([]installation.Spec, error) {
	return s.installationSpecsByQuery(ctx, `SELECT id FROM installation_specs ORDER BY created_at DESC,id DESC`)
}

func (s *Store) InstallationSpecsForMachine(ctx context.Context, machineID string) ([]installation.Spec, error) {
	return s.installationSpecsByQuery(ctx, `SELECT id FROM installation_specs WHERE machine_id=? ORDER BY created_at DESC,id DESC`, strings.TrimSpace(machineID))
}

func (s *Store) installationSpecsByQuery(ctx context.Context, query string, args ...any) ([]installation.Spec, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, s.storageError("list installation specs", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, s.storageError("read installation spec identifier", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, s.storageError("iterate installation spec identifiers", err)
	}
	if err := rows.Close(); err != nil {
		return nil, s.storageError("close installation spec list", err)
	}

	items := make([]installation.Spec, 0, len(ids))
	for _, id := range ids {
		item, err := s.InstallationSpec(ctx, id)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
