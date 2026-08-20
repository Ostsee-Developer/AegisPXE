package store

import (
	"context"
	"database/sql"
	"fmt"
)

func applyManagedAgentSchema(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS agents (
			id TEXT PRIMARY KEY,
			installation_id TEXT NOT NULL UNIQUE REFERENCES installation_specs(id) ON DELETE CASCADE,
			machine_id TEXT NOT NULL REFERENCES machines(id) ON DELETE RESTRICT,
			state TEXT NOT NULL CHECK(state IN ('pending_build','ready','unenrolled','enrolling','online','degraded','offline','revoked')),
			update_mode TEXT NOT NULL DEFAULT 'manual' CHECK(update_mode IN ('manual','automatic')),
			update_state TEXT NOT NULL DEFAULT 'idle' CHECK(update_state IN ('idle','available','downloading','verifying','staging','installing','restarting','confirming','success','failed','rollback')),
			capability_ceiling_json TEXT NOT NULL DEFAULT '[]',
			active_generation INTEGER NOT NULL DEFAULT 0 CHECK(active_generation >= 0),
			desired_generation INTEGER NOT NULL DEFAULT 1 CHECK(desired_generation >= 1),
			active_version TEXT NOT NULL DEFAULT '',
			last_seen_at TEXT NOT NULL DEFAULT '',
			last_heartbeat_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			revoked_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_agents_machine ON agents(machine_id,created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_agents_state ON agents(state,updated_at)`,
		`CREATE TABLE IF NOT EXISTS agent_builds (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
			generation INTEGER NOT NULL CHECK(generation > 0),
			version TEXT NOT NULL,
			architecture TEXT NOT NULL,
			capability_ceiling_json TEXT NOT NULL DEFAULT '[]',
			state TEXT NOT NULL CHECK(state IN ('queued','building','ready','failed','superseded','revoked')),
			package_path TEXT NOT NULL DEFAULT '',
			package_sha256 TEXT NOT NULL DEFAULT '',
			package_size INTEGER NOT NULL DEFAULT 0 CHECK(package_size >= 0),
			manifest_sha256 TEXT NOT NULL DEFAULT '',
			manifest_signature BLOB NOT NULL DEFAULT X'',
			created_at TEXT NOT NULL,
			started_at TEXT NOT NULL DEFAULT '',
			ready_at TEXT NOT NULL DEFAULT '',
			failed_at TEXT NOT NULL DEFAULT '',
			superseded_at TEXT NOT NULL DEFAULT '',
			revoked_at TEXT NOT NULL DEFAULT '',
			error_code TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			UNIQUE(agent_id,generation)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_builds_state ON agent_builds(state,created_at)`,
		`CREATE TABLE IF NOT EXISTS agent_enrollment_credentials (
			credential_id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
			secret_sha256 BLOB NOT NULL CHECK(length(secret_sha256) = 32),
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			consumed_at TEXT NOT NULL DEFAULT '',
			revoked_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_enrollment_active
		 ON agent_enrollment_credentials(agent_id) WHERE consumed_at='' AND revoked_at=''`,
		`CREATE TABLE IF NOT EXISTS agent_certificates (
			fingerprint TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
			serial TEXT NOT NULL UNIQUE,
			public_key_sha256 TEXT NOT NULL,
			issued_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			revoked_at TEXT NOT NULL DEFAULT '',
			revoked_by TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_certificates_agent ON agent_certificates(agent_id,issued_at)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply managed agent schema: %w", err)
		}
	}
	return nil
}
