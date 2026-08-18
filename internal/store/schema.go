package store

import (
	"context"
	"database/sql"
	"fmt"
)

const currentSchemaVersion = 2

func (s *Store) initialize(ctx context.Context) error {
	for _, pragma := range []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA journal_mode = WAL`,
		`PRAGMA busy_timeout = 5000`,
	} {
		if _, err := s.db.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("sqlite pragma: %w", err)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema transaction: %w", err)
	}
	defer tx.Rollback()

	statements := []string{
		`CREATE TABLE IF NOT EXISTS schema_meta (
			version INTEGER NOT NULL
		)`,
		`INSERT INTO schema_meta(version)
		 SELECT 1 WHERE NOT EXISTS (SELECT 1 FROM schema_meta)`,
		`CREATE TABLE IF NOT EXISTS machines (
			id TEXT PRIMARY KEY,
			policy TEXT NOT NULL CHECK(policy IN ('pending','local','provision','blocked')),
			architecture TEXT NOT NULL DEFAULT '',
			firmware TEXT NOT NULL DEFAULT '',
			first_seen TEXT NOT NULL,
			last_seen TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS machine_identifiers (
			machine_id TEXT NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
			kind TEXT NOT NULL,
			value TEXT NOT NULL,
			first_seen TEXT NOT NULL,
			last_seen TEXT NOT NULL,
			PRIMARY KEY(machine_id, kind, value),
			UNIQUE(kind, value)
		)`,
		`CREATE TABLE IF NOT EXISTS installation_specs (
			id TEXT PRIMARY KEY,
			machine_id TEXT NOT NULL REFERENCES machines(id) ON DELETE RESTRICT,
			driver_id TEXT NOT NULL,
			driver_version TEXT NOT NULL,
			os_release TEXT NOT NULL,
			architecture TEXT NOT NULL,
			profile_id TEXT NOT NULL,
			profile_revision TEXT NOT NULL,
			profile_json TEXT NOT NULL DEFAULT '{}',
			artifacts_json TEXT NOT NULL,
			storage_json TEXT NOT NULL,
			security_json TEXT NOT NULL,
			lifecycle_credential_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			created_by TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_installation_specs_machine ON installation_specs(machine_id, created_at)`,
		`CREATE TABLE IF NOT EXISTS events (
			sequence INTEGER PRIMARY KEY AUTOINCREMENT,
			entity_type TEXT NOT NULL,
			entity_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			occurred_at TEXT NOT NULL,
			request_id TEXT NOT NULL DEFAULT '',
			actor TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			error_code TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_entity ON events(entity_type, entity_id, sequence)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply schema: %w", err)
		}
	}

	var version int
	if err := tx.QueryRowContext(ctx, `SELECT version FROM schema_meta LIMIT 1`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > currentSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than supported version %d", version, currentSchemaVersion)
	}

	hasProfileJSON, err := columnExists(ctx, tx, "installation_specs", "profile_json")
	if err != nil {
		return fmt.Errorf("inspect installation schema: %w", err)
	}
	if !hasProfileJSON {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE installation_specs ADD COLUMN profile_json TEXT NOT NULL DEFAULT '{}'`); err != nil {
			return fmt.Errorf("migrate installation profile snapshot: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE schema_meta SET version=?`, currentSchemaVersion); err != nil {
		return fmt.Errorf("update schema version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema: %w", err)
	}
	return nil
}

func columnExists(ctx context.Context, tx *sql.Tx, table, column string) (bool, error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}
