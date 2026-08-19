package store

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/observability"
)

func TestSchemaMigrationAddsProvisioningAndOperatorIdentitySchemaWithLogs(t *testing.T) {
	path := t.TempDir() + "/aegispxe-v1.db"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE schema_meta (version INTEGER NOT NULL)`,
		`INSERT INTO schema_meta(version) VALUES(1)`,
		`CREATE TABLE installation_specs (
			id TEXT PRIMARY KEY,
			machine_id TEXT NOT NULL,
			driver_id TEXT NOT NULL,
			driver_version TEXT NOT NULL,
			os_release TEXT NOT NULL,
			architecture TEXT NOT NULL,
			profile_id TEXT NOT NULL,
			profile_revision TEXT NOT NULL,
			artifacts_json TEXT NOT NULL,
			storage_json TEXT NOT NULL,
			security_json TEXT NOT NULL,
			lifecycle_credential_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			created_by TEXT NOT NULL
		)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	state, err := Open(context.Background(), path, observability.New(&logs, slog.LevelDebug))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()

	var version int
	if err := state.db.QueryRow(`SELECT version FROM schema_meta LIMIT 1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != currentSchemaVersion {
		t.Fatalf("schema version=%d want=%d", version, currentSchemaVersion)
	}
	hasProfileJSON, err := columnExistsForTest(state.db, "installation_specs", "profile_json")
	if err != nil {
		t.Fatal(err)
	}
	if !hasProfileJSON {
		t.Fatal("profile_json column was not added")
	}
	if !tableExistsForTest(t, state.db, "installation_assignments") {
		t.Fatal("installation_assignments table was not added")
	}
	if !tableExistsForTest(t, state.db, "operator_users") || !tableExistsForTest(t, state.db, "operator_credentials") {
		t.Fatal("operator identity tables were not added")
	}
	for _, table := range []string{
		"installation_lifecycle_credentials",
		"installation_lifecycle_events",
		"installation_log_chunks",
		"machine_boot_trust_keys",
		"installation_boot_trust_challenges",
	} {
		if !tableExistsForTest(t, state.db, table) {
			t.Fatalf("%s table was not added", table)
		}
	}
	logText := logs.String()
	if !strings.Contains(logText, `"component":"store.schema"`) ||
		!strings.Contains(logText, `"operation":"migrate"`) ||
		!strings.Contains(logText, `"from_version":1`) ||
		!strings.Contains(logText, `"to_version":6`) ||
		!strings.Contains(logText, `"assignment_schema_added":true`) ||
		!strings.Contains(logText, `"operator_identity_schema_added":true`) ||
		!strings.Contains(logText, `"installer_telemetry_schema_added":true`) ||
		!strings.Contains(logText, `"boot_trust_schema_added":true`) ||
		!strings.Contains(logText, `"result":"success"`) {
		t.Fatalf("migration log missing contract fields: %s", logText)
	}
}

func columnExistsForTest(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
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
	return false, rows.Err()
}

func tableExistsForTest(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var name string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
		if err == sql.ErrNoRows {
			return false
		}
		t.Fatal(err)
	}
	return name == table
}
