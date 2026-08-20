package store

import (
	"context"
	"database/sql"
	"fmt"
)

const currentSchemaVersion = 8

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
		 SELECT 8 WHERE NOT EXISTS (SELECT 1 FROM schema_meta)`,
		`CREATE TABLE IF NOT EXISTS machines (
			id TEXT PRIMARY KEY,
			nickname TEXT NOT NULL DEFAULT '',
			policy TEXT NOT NULL CHECK(policy IN ('pending','local','provision','blocked')),
			architecture TEXT NOT NULL DEFAULT '',
			firmware TEXT NOT NULL DEFAULT '',
			secure_boot_state TEXT NOT NULL DEFAULT 'unknown' CHECK(secure_boot_state IN ('unknown','enabled','disabled','setup_mode','unsupported')),
			secure_boot_observed_at TEXT NOT NULL DEFAULT '',
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
		`CREATE TABLE IF NOT EXISTS installation_assignments (
			id TEXT PRIMARY KEY,
			machine_id TEXT NOT NULL REFERENCES machines(id) ON DELETE RESTRICT,
			installation_id TEXT NOT NULL UNIQUE REFERENCES installation_specs(id) ON DELETE RESTRICT,
			state TEXT NOT NULL CHECK(state IN ('armed','consumed','cancelled')),
			trust_requirement TEXT NOT NULL CHECK(trust_requirement = 'cryptographic'),
			armed_at TEXT NOT NULL,
			armed_by TEXT NOT NULL,
			consumed_at TEXT NOT NULL DEFAULT '',
			cancelled_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_installation_assignments_armed_machine
		 ON installation_assignments(machine_id) WHERE state='armed'`,
		`CREATE INDEX IF NOT EXISTS idx_installation_assignments_installation ON installation_assignments(installation_id)`,
		`CREATE TABLE IF NOT EXISTS operator_users (
			id TEXT PRIMARY KEY,
			provider TEXT NOT NULL,
			subject TEXT NOT NULL,
			display_name TEXT NOT NULL,
			email TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL DEFAULT '' CHECK(role IN ('','admin','operator')),
			status TEXT NOT NULL CHECK(status IN ('pending_review','enrollment_required','active','blocked')),
			webauthn_handle BLOB NOT NULL UNIQUE,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			approved_at TEXT NOT NULL DEFAULT '',
			approved_by TEXT NOT NULL DEFAULT '',
			UNIQUE(provider,subject)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_operator_users_status ON operator_users(status,created_at)`,
		`CREATE TABLE IF NOT EXISTS operator_credentials (
			user_id TEXT NOT NULL REFERENCES operator_users(id) ON DELETE CASCADE,
			rp_id TEXT NOT NULL,
			credential_id BLOB NOT NULL,
			credential_json BLOB NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			last_used_at TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(user_id,rp_id,credential_id),
			UNIQUE(rp_id,credential_id)
		)`,
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
		`CREATE TABLE IF NOT EXISTS installation_lifecycle_credentials (
			credential_id TEXT PRIMARY KEY,
			installation_id TEXT NOT NULL UNIQUE REFERENCES installation_specs(id) ON DELETE RESTRICT,
			secret_sha256 BLOB NOT NULL CHECK(length(secret_sha256) = 32),
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			revoked_at TEXT NOT NULL DEFAULT '',
			last_used_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_installation_lifecycle_credentials_installation
		 ON installation_lifecycle_credentials(installation_id)`,
		`CREATE TABLE IF NOT EXISTS installation_lifecycle_events (
			sequence INTEGER PRIMARY KEY AUTOINCREMENT,
			installation_id TEXT NOT NULL REFERENCES installation_specs(id) ON DELETE RESTRICT,
			stage TEXT NOT NULL CHECK(stage IN ('CREATED','QUEUED','PXE_BOOTED','INSTALLER_STARTED','DISK_PREPARATION','OS_INSTALLING','PROFILE_APPLYING','HARDENING','FIRST_BOOT','VALIDATING','SUCCESS','FAILED')),
			source TEXT NOT NULL CHECK(source IN ('server','installer','finalizer','validator')),
			received_at TEXT NOT NULL,
			client_at TEXT NOT NULL DEFAULT '',
			request_id TEXT NOT NULL DEFAULT '',
			idempotency_key TEXT NOT NULL,
			message TEXT NOT NULL DEFAULT '',
			error_code TEXT NOT NULL DEFAULT '',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			UNIQUE(installation_id,idempotency_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_installation_lifecycle_events_installation
		 ON installation_lifecycle_events(installation_id,sequence)`,
		`CREATE TABLE IF NOT EXISTS installation_log_chunks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			installation_id TEXT NOT NULL REFERENCES installation_specs(id) ON DELETE RESTRICT,
			sequence INTEGER NOT NULL,
			source TEXT NOT NULL CHECK(source IN ('installer','finalizer','validator')),
			received_at TEXT NOT NULL,
			client_at TEXT NOT NULL DEFAULT '',
			request_id TEXT NOT NULL DEFAULT '',
			idempotency_key TEXT NOT NULL,
			content TEXT NOT NULL,
			content_sha256 TEXT NOT NULL,
			UNIQUE(installation_id,sequence),
			UNIQUE(installation_id,idempotency_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_installation_log_chunks_installation
		 ON installation_log_chunks(installation_id,sequence)`,
		`CREATE TABLE IF NOT EXISTS machine_boot_trust_keys (
			fingerprint TEXT PRIMARY KEY,
			machine_id TEXT NOT NULL REFERENCES machines(id) ON DELETE RESTRICT,
			public_key_pem TEXT NOT NULL,
			ek_fingerprint TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL CHECK(state IN ('pending','approved','revoked')),
			first_seen_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			approved_at TEXT NOT NULL DEFAULT '',
			approved_by TEXT NOT NULL DEFAULT '',
			revoked_at TEXT NOT NULL DEFAULT '',
			revoked_by TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_machine_boot_trust_keys_machine ON machine_boot_trust_keys(machine_id,state,first_seen_at)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_machine_boot_trust_keys_approved_machine ON machine_boot_trust_keys(machine_id) WHERE state='approved'`,
		`CREATE TABLE IF NOT EXISTS installation_boot_trust_challenges (
			id TEXT PRIMARY KEY,
			installation_id TEXT NOT NULL REFERENCES installation_specs(id) ON DELETE RESTRICT,
			machine_id TEXT NOT NULL REFERENCES machines(id) ON DELETE RESTRICT,
			key_fingerprint TEXT NOT NULL REFERENCES machine_boot_trust_keys(fingerprint) ON DELETE RESTRICT,
			nonce BLOB NOT NULL CHECK(length(nonce)=32),
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			used_at TEXT NOT NULL DEFAULT '',
			response_ciphertext BLOB NOT NULL DEFAULT X'',
			credential_expires_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_installation_boot_trust_challenges_installation ON installation_boot_trust_challenges(installation_id,created_at)`,
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
	fromVersion := version

	hasProfileJSON, err := columnExists(ctx, tx, "installation_specs", "profile_json")
	if err != nil {
		return fmt.Errorf("inspect installation schema: %w", err)
	}
	profileColumnAdded := !hasProfileJSON
	if profileColumnAdded {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE installation_specs ADD COLUMN profile_json TEXT NOT NULL DEFAULT '{}'`); err != nil {
			return fmt.Errorf("migrate installation profile snapshot: %w", err)
		}
	}

	hasNickname, err := columnExists(ctx, tx, "machines", "nickname")
	if err != nil {
		return fmt.Errorf("inspect machine nickname schema: %w", err)
	}
	nicknameColumnAdded := !hasNickname
	if nicknameColumnAdded {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE machines ADD COLUMN nickname TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("migrate machine nickname: %w", err)
		}
	}

	hasSecureBootState, err := columnExists(ctx, tx, "machines", "secure_boot_state")
	if err != nil {
		return fmt.Errorf("inspect machine Secure Boot schema: %w", err)
	}
	secureBootStateColumnAdded := !hasSecureBootState
	if secureBootStateColumnAdded {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE machines ADD COLUMN secure_boot_state TEXT NOT NULL DEFAULT 'unknown' CHECK(secure_boot_state IN ('unknown','enabled','disabled','setup_mode','unsupported'))`); err != nil {
			return fmt.Errorf("migrate machine Secure Boot state: %w", err)
		}
	}

	hasSecureBootObservedAt, err := columnExists(ctx, tx, "machines", "secure_boot_observed_at")
	if err != nil {
		return fmt.Errorf("inspect machine Secure Boot observation schema: %w", err)
	}
	secureBootObservedAtColumnAdded := !hasSecureBootObservedAt
	if secureBootObservedAtColumnAdded {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE machines ADD COLUMN secure_boot_observed_at TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("migrate machine Secure Boot observation timestamp: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `UPDATE schema_meta SET version=?`, currentSchemaVersion); err != nil {
		return fmt.Errorf("update schema version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema: %w", err)
	}
	if fromVersion != currentSchemaVersion || profileColumnAdded || nicknameColumnAdded || secureBootStateColumnAdded || secureBootObservedAtColumnAdded {
		s.logger.InfoContext(ctx, "storage schema migrated",
			"component", "store.schema",
			"operation", "migrate",
			"from_version", fromVersion,
			"to_version", currentSchemaVersion,
			"profile_snapshot_column_added", profileColumnAdded,
			"machine_nickname_column_added", nicknameColumnAdded,
			"machine_secure_boot_state_column_added", secureBootStateColumnAdded,
			"machine_secure_boot_observed_at_column_added", secureBootObservedAtColumnAdded,
			"assignment_schema_added", fromVersion < 3,
			"operator_identity_schema_added", fromVersion < 4,
			"installer_telemetry_schema_added", fromVersion < 5,
			"boot_trust_schema_added", fromVersion < 6,
			"result", "success",
		)
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
