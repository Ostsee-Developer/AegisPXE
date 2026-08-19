package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/lifecycle"
	"github.com/Ostsee-Developer/AegisPXE/internal/telemetry"
)

const defaultLifecycleCredentialTTL = 24 * time.Hour

func (s *Store) IssueLifecycleCredential(ctx context.Context, installationID, requestID, actor string, ttl time.Duration) (telemetry.IssuedCredential, error) {
	installationID = strings.TrimSpace(installationID)
	actor = strings.TrimSpace(actor)
	if installationID == "" || actor == "" {
		return telemetry.IssuedCredential{}, fault.New(fault.InstallerTelemetryInvalid, "installation and credential issuer are required", nil)
	}
	if ttl <= 0 {
		ttl = defaultLifecycleCredentialTTL
	}
	if ttl < 5*time.Minute || ttl > 7*24*time.Hour {
		return telemetry.IssuedCredential{}, fault.New(fault.InstallerTelemetryInvalid, "lifecycle credential lifetime is outside allowed bounds", nil)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return telemetry.IssuedCredential{}, s.storageError("begin lifecycle credential issue", err)
	}
	defer tx.Rollback()

	var credentialID string
	if err := tx.QueryRowContext(ctx, `SELECT lifecycle_credential_id FROM installation_specs WHERE id=?`, installationID).Scan(&credentialID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return telemetry.IssuedCredential{}, fault.New(fault.InstallationNotFound, "installation not found", err)
		}
		return telemetry.IssuedCredential{}, s.storageError("read lifecycle credential identifier", err)
	}
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT credential_id FROM installation_lifecycle_credentials WHERE installation_id=?`, installationID).Scan(&existing)
	if err == nil {
		return telemetry.IssuedCredential{}, fault.New(fault.InstallerTelemetryConflict, "installation lifecycle credential already exists", nil)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return telemetry.IssuedCredential{}, s.storageError("check lifecycle credential", err)
	}

	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return telemetry.IssuedCredential{}, fault.New(fault.StorageFailure, "could not generate lifecycle credential", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)
	hash := sha256.Sum256([]byte(secret))
	now := s.now().UTC()
	expiresAt := now.Add(ttl)
	if _, err := tx.ExecContext(ctx, `INSERT INTO installation_lifecycle_credentials(
		credential_id,installation_id,secret_sha256,created_at,expires_at,revoked_at,last_used_at
	) VALUES(?,?,?,?,?,?,?)`, credentialID, installationID, hash[:], now.Format(time.RFC3339Nano), expiresAt.Format(time.RFC3339Nano), "", ""); err != nil {
		return telemetry.IssuedCredential{}, s.storageError("persist lifecycle credential", err)
	}
	if err := appendEventTx(ctx, tx, event.Event{
		EntityType: event.EntityInstallation,
		EntityID:   installationID,
		Type:       "INSTALLER_CREDENTIAL_ISSUED",
		OccurredAt: now,
		RequestID:  requestID,
		Actor:      actor,
		Message:    "installation-scoped lifecycle credential issued behind trust boundary",
	}); err != nil {
		return telemetry.IssuedCredential{}, s.storageError("persist lifecycle credential audit event", err)
	}
	if err := tx.Commit(); err != nil {
		return telemetry.IssuedCredential{}, s.storageError("commit lifecycle credential issue", err)
	}

	item := telemetry.Credential{ID: credentialID, InstallationID: installationID, CreatedAt: now, ExpiresAt: expiresAt}
	s.logger.InfoContext(ctx, "installation lifecycle credential issued",
		"component", "store.telemetry",
		"operation", "issue_credential",
		"request_id", requestID,
		"installation_id", installationID,
		"credential_id", credentialID,
		"actor", actor,
		"expires_at", expiresAt,
		"result", "success",
	)
	return telemetry.IssuedCredential{Credential: item, Secret: secret}, nil
}

func (s *Store) AuthenticateLifecycleCredential(ctx context.Context, installationID, secret string) (telemetry.Credential, error) {
	installationID = strings.TrimSpace(installationID)
	secret = strings.TrimSpace(secret)
	if installationID == "" || secret == "" || len(secret) > 256 {
		return telemetry.Credential{}, fault.New(fault.InstallerCredentialInvalid, "installer credential is invalid", nil)
	}
	row := s.db.QueryRowContext(ctx, `SELECT credential_id,installation_id,secret_sha256,created_at,expires_at,revoked_at,last_used_at
		FROM installation_lifecycle_credentials WHERE installation_id=?`, installationID)
	var item telemetry.Credential
	var expected []byte
	var createdAt, expiresAt, revokedAt, lastUsedAt string
	if err := row.Scan(&item.ID, &item.InstallationID, &expected, &createdAt, &expiresAt, &revokedAt, &lastUsedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return telemetry.Credential{}, fault.New(fault.InstallerCredentialInvalid, "installer credential is invalid", nil)
		}
		return telemetry.Credential{}, s.storageError("read lifecycle credential", err)
	}
	actual := sha256.Sum256([]byte(secret))
	if len(expected) != len(actual) || subtle.ConstantTimeCompare(expected, actual[:]) != 1 {
		return telemetry.Credential{}, fault.New(fault.InstallerCredentialInvalid, "installer credential is invalid", nil)
	}
	var err error
	item.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return telemetry.Credential{}, s.storageError("parse lifecycle credential creation time", err)
	}
	item.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return telemetry.Credential{}, s.storageError("parse lifecycle credential expiry time", err)
	}
	if revokedAt != "" {
		item.RevokedAt, err = time.Parse(time.RFC3339Nano, revokedAt)
		if err != nil {
			return telemetry.Credential{}, s.storageError("parse lifecycle credential revocation time", err)
		}
	}
	if lastUsedAt != "" {
		item.LastUsedAt, err = time.Parse(time.RFC3339Nano, lastUsedAt)
		if err != nil {
			return telemetry.Credential{}, s.storageError("parse lifecycle credential last-used time", err)
		}
	}
	now := s.now().UTC()
	if !item.Active(now) {
		return telemetry.Credential{}, fault.New(fault.InstallerCredentialExpired, "installer credential is expired or revoked", nil)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE installation_lifecycle_credentials SET last_used_at=? WHERE credential_id=?`, now.Format(time.RFC3339Nano), item.ID); err != nil {
		return telemetry.Credential{}, s.storageError("update lifecycle credential last-used time", err)
	}
	item.LastUsedAt = now
	return item, nil
}

func (s *Store) RevokeLifecycleCredential(ctx context.Context, installationID, requestID, actor string) error {
	installationID = strings.TrimSpace(installationID)
	actor = strings.TrimSpace(actor)
	if installationID == "" || actor == "" {
		return fault.New(fault.InstallerTelemetryInvalid, "installation and credential revoker are required", nil)
	}
	now := s.now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE installation_lifecycle_credentials SET revoked_at=?
		WHERE installation_id=? AND revoked_at=''`, now.Format(time.RFC3339Nano), installationID)
	if err != nil {
		return s.storageError("revoke lifecycle credential", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return s.storageError("read lifecycle credential revoke result", err)
	}
	if rows == 0 {
		return fault.New(fault.InstallerCredentialInvalid, "active installer credential not found", nil)
	}
	s.logger.InfoContext(ctx, "installation lifecycle credential revoked",
		"component", "store.telemetry",
		"operation", "revoke_credential",
		"request_id", requestID,
		"installation_id", installationID,
		"actor", actor,
		"result", "success",
	)
	return nil
}

func (s *Store) AppendLifecycleEvent(ctx context.Context, report lifecycle.Report, requestID string) (lifecycle.Event, bool, error) {
	if err := report.Validate(); err != nil {
		return lifecycle.Event{}, false, fault.New(fault.InstallerTelemetryInvalid, "lifecycle report is invalid", err)
	}
	metadataJSON, err := json.Marshal(report.Metadata)
	if err != nil {
		return lifecycle.Event{}, false, fault.New(fault.InstallerTelemetryInvalid, "lifecycle metadata is invalid", err)
	}
	if string(metadataJSON) == "null" {
		metadataJSON = []byte("{}")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return lifecycle.Event{}, false, s.storageError("begin lifecycle event", err)
	}
	defer tx.Rollback()

	if err := ensureInstallationExistsTx(ctx, tx, report.InstallationID); err != nil {
		return lifecycle.Event{}, false, err
	}

	existing, err := lifecycleEventByIdempotencyTx(ctx, tx, report.InstallationID, report.IdempotencyKey)
	if err == nil {
		existingMetadata, _ := json.Marshal(existing.Metadata)
		if string(existingMetadata) != "null" && string(existingMetadata) != string(metadataJSON) ||
			existing.Stage != report.Stage || existing.Source != report.Source || existing.Message != report.Message || existing.ErrorCode != report.ErrorCode || !sameOptionalTime(existing.ClientAt, report.ClientAt) {
			return lifecycle.Event{}, false, fault.New(fault.InstallerTelemetryConflict, "idempotency key was reused with different lifecycle content", nil)
		}
		return existing, true, nil
	}
	if fault.Code(err) != fault.InstallationNotFound {
		return lifecycle.Event{}, false, err
	}

	current, err := currentLifecycleStageTx(ctx, tx, report.InstallationID)
	if err != nil {
		return lifecycle.Event{}, false, err
	}
	if !lifecycle.CanAdvance(current, report.Stage) {
		return lifecycle.Event{}, false, fault.New(fault.InstallerTelemetryConflict, "lifecycle event would regress or skip required state", nil)
	}

	now := s.now().UTC()
	clientAt := ""
	if !report.ClientAt.IsZero() {
		clientAt = report.ClientAt.UTC().Format(time.RFC3339Nano)
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO installation_lifecycle_events(
		installation_id,stage,source,received_at,client_at,request_id,idempotency_key,message,error_code,metadata_json
	) VALUES(?,?,?,?,?,?,?,?,?,?)`, report.InstallationID, report.Stage, report.Source, now.Format(time.RFC3339Nano), clientAt,
		requestID, report.IdempotencyKey, report.Message, report.ErrorCode, string(metadataJSON))
	if err != nil {
		return lifecycle.Event{}, false, s.storageError("persist lifecycle event", err)
	}
	sequence, err := result.LastInsertId()
	if err != nil {
		return lifecycle.Event{}, false, s.storageError("read lifecycle event sequence", err)
	}
	message := report.Message
	if strings.TrimSpace(message) == "" {
		message = "authoritative lifecycle stage accepted: " + string(report.Stage)
	}
	if err := appendEventTx(ctx, tx, event.Event{
		EntityType: event.EntityInstallation,
		EntityID:   report.InstallationID,
		Type:       string(report.Stage),
		OccurredAt: now,
		RequestID:  requestID,
		Actor:      "telemetry:" + string(report.Source),
		Message:    message,
		ErrorCode:  report.ErrorCode,
	}); err != nil {
		return lifecycle.Event{}, false, s.storageError("persist lifecycle audit projection", err)
	}
	if report.Stage.Terminal() {
		if _, err := tx.ExecContext(ctx, `UPDATE installation_lifecycle_credentials SET revoked_at=?
			WHERE installation_id=? AND revoked_at=''`, now.Format(time.RFC3339Nano), report.InstallationID); err != nil {
			return lifecycle.Event{}, false, s.storageError("revoke terminal lifecycle credential", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return lifecycle.Event{}, false, s.storageError("commit lifecycle event", err)
	}

	accepted := lifecycle.Event{
		Sequence:       sequence,
		InstallationID: report.InstallationID,
		Stage:          report.Stage,
		Source:         report.Source,
		ReceivedAt:     now,
		ClientAt:       report.ClientAt.UTC(),
		RequestID:      requestID,
		IdempotencyKey: report.IdempotencyKey,
		Message:        report.Message,
		ErrorCode:      report.ErrorCode,
		Metadata:       cloneMetadata(report.Metadata),
	}
	s.logger.InfoContext(ctx, "installer lifecycle event accepted",
		"component", "store.telemetry",
		"operation", "append_lifecycle",
		"request_id", requestID,
		"installation_id", report.InstallationID,
		"event_seq", sequence,
		"stage", report.Stage,
		"source", report.Source,
		"result", "success",
	)
	return accepted, false, nil
}

func (s *Store) RecordServerLifecycle(ctx context.Context, installationID string, stage lifecycle.Stage, idempotencyKey, message, requestID string) (lifecycle.Event, bool, error) {
	return s.AppendLifecycleEvent(ctx, lifecycle.Report{
		InstallationID: installationID,
		Stage:          stage,
		Source:         lifecycle.SourceServer,
		IdempotencyKey: idempotencyKey,
		Message:        message,
	}, requestID)
}

func (s *Store) LifecycleEvents(ctx context.Context, installationID string, limit int) ([]lifecycle.Event, error) {
	installationID = strings.TrimSpace(installationID)
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT sequence,installation_id,stage,source,received_at,client_at,request_id,idempotency_key,message,error_code,metadata_json
		FROM installation_lifecycle_events WHERE installation_id=? ORDER BY sequence ASC LIMIT ?`, installationID, limit)
	if err != nil {
		return nil, s.storageError("list lifecycle events", err)
	}
	defer rows.Close()
	var items []lifecycle.Event
	for rows.Next() {
		item, err := scanLifecycleEvent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, s.storageError("iterate lifecycle events", err)
	}
	return items, nil
}

func (s *Store) CurrentLifecycleStage(ctx context.Context, installationID string) (lifecycle.Stage, error) {
	row := s.db.QueryRowContext(ctx, `SELECT stage FROM installation_lifecycle_events WHERE installation_id=? ORDER BY sequence DESC LIMIT 1`, strings.TrimSpace(installationID))
	var stage lifecycle.Stage
	if err := row.Scan(&stage); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", s.storageError("read current lifecycle stage", err)
	}
	return stage, nil
}

func (s *Store) AppendInstallationLogChunk(ctx context.Context, chunk telemetry.LogChunk) (telemetry.LogChunk, bool, error) {
	chunk.Content = telemetry.RedactLogContent(chunk.Content)
	if err := chunk.Validate(); err != nil {
		return telemetry.LogChunk{}, false, fault.New(fault.InstallerTelemetryInvalid, "installer log chunk is invalid", err)
	}
	digestRaw := sha256.Sum256([]byte(chunk.Content))
	chunk.Digest = "sha256:" + hex.EncodeToString(digestRaw[:])

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return telemetry.LogChunk{}, false, s.storageError("begin installer log chunk", err)
	}
	defer tx.Rollback()
	if err := ensureInstallationExistsTx(ctx, tx, chunk.InstallationID); err != nil {
		return telemetry.LogChunk{}, false, err
	}

	existing, err := installationLogByIdempotencyTx(ctx, tx, chunk.InstallationID, chunk.IdempotencyKey)
	if err == nil {
		if existing.Sequence != chunk.Sequence || existing.Source != chunk.Source || existing.Digest != chunk.Digest || !sameOptionalTime(existing.ClientAt, chunk.ClientAt) {
			return telemetry.LogChunk{}, false, fault.New(fault.InstallerTelemetryConflict, "idempotency key was reused with different log content", nil)
		}
		return existing, true, nil
	}
	if fault.Code(err) != fault.InstallationNotFound {
		return telemetry.LogChunk{}, false, err
	}

	var lastSequence int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0) FROM installation_log_chunks WHERE installation_id=?`, chunk.InstallationID).Scan(&lastSequence); err != nil {
		return telemetry.LogChunk{}, false, s.storageError("read installer log sequence", err)
	}
	if chunk.Sequence != lastSequence+1 {
		return telemetry.LogChunk{}, false, fault.New(fault.InstallerTelemetryConflict, "installer log sequence is not contiguous", nil)
	}
	var totalBytes int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(length(CAST(content AS BLOB))),0) FROM installation_log_chunks WHERE installation_id=?`, chunk.InstallationID).Scan(&totalBytes); err != nil {
		return telemetry.LogChunk{}, false, s.storageError("read installer log size", err)
	}
	if totalBytes+int64(len(chunk.Content)) > telemetry.MaxInstallationLogBytes {
		return telemetry.LogChunk{}, false, fault.New(fault.InstallerLogLimitExceeded, "installation log storage limit exceeded", nil)
	}
	now := s.now().UTC()
	clientAt := ""
	if !chunk.ClientAt.IsZero() {
		clientAt = chunk.ClientAt.UTC().Format(time.RFC3339Nano)
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO installation_log_chunks(
		installation_id,sequence,source,received_at,client_at,request_id,idempotency_key,content,content_sha256
	) VALUES(?,?,?,?,?,?,?,?,?)`, chunk.InstallationID, chunk.Sequence, chunk.Source, now.Format(time.RFC3339Nano), clientAt,
		chunk.RequestID, chunk.IdempotencyKey, chunk.Content, chunk.Digest)
	if err != nil {
		return telemetry.LogChunk{}, false, s.storageError("persist installer log chunk", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return telemetry.LogChunk{}, false, s.storageError("read installer log chunk ID", err)
	}
	if err := tx.Commit(); err != nil {
		return telemetry.LogChunk{}, false, s.storageError("commit installer log chunk", err)
	}
	chunk.ID = id
	chunk.ReceivedAt = now
	s.logger.InfoContext(ctx, "installer log chunk accepted",
		"component", "store.telemetry",
		"operation", "append_log_chunk",
		"request_id", chunk.RequestID,
		"installation_id", chunk.InstallationID,
		"log_sequence", chunk.Sequence,
		"source", chunk.Source,
		"bytes", len(chunk.Content),
		"digest", chunk.Digest,
		"result", "success",
	)
	return chunk, false, nil
}

func (s *Store) InstallationLogChunks(ctx context.Context, installationID string, limit int) ([]telemetry.LogChunk, error) {
	installationID = strings.TrimSpace(installationID)
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,installation_id,sequence,source,received_at,client_at,request_id,idempotency_key,content,content_sha256
		FROM installation_log_chunks WHERE installation_id=? ORDER BY sequence DESC LIMIT ?`, installationID, limit)
	if err != nil {
		return nil, s.storageError("list installer log chunks", err)
	}
	defer rows.Close()
	var reversed []telemetry.LogChunk
	for rows.Next() {
		item, err := scanInstallationLogChunk(rows)
		if err != nil {
			return nil, err
		}
		reversed = append(reversed, item)
	}
	if err := rows.Err(); err != nil {
		return nil, s.storageError("iterate installer log chunks", err)
	}
	items := make([]telemetry.LogChunk, len(reversed))
	for index := range reversed {
		items[len(reversed)-1-index] = reversed[index]
	}
	return items, nil
}

func ensureInstallationExistsTx(ctx context.Context, tx *sql.Tx, installationID string) error {
	var value string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM installation_specs WHERE id=?`, installationID).Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fault.New(fault.InstallationNotFound, "installation not found", err)
		}
		return fault.New(fault.StorageFailure, "could not read installation", err)
	}
	return nil
}

func currentLifecycleStageTx(ctx context.Context, tx *sql.Tx, installationID string) (lifecycle.Stage, error) {
	var stage lifecycle.Stage
	if err := tx.QueryRowContext(ctx, `SELECT stage FROM installation_lifecycle_events WHERE installation_id=? ORDER BY sequence DESC LIMIT 1`, installationID).Scan(&stage); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fault.New(fault.StorageFailure, "could not read current lifecycle stage", err)
	}
	return stage, nil
}

func lifecycleEventByIdempotencyTx(ctx context.Context, tx *sql.Tx, installationID, key string) (lifecycle.Event, error) {
	row := tx.QueryRowContext(ctx, `SELECT sequence,installation_id,stage,source,received_at,client_at,request_id,idempotency_key,message,error_code,metadata_json
		FROM installation_lifecycle_events WHERE installation_id=? AND idempotency_key=?`, installationID, key)
	item, err := scanLifecycleEvent(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || fault.Code(err) == fault.InstallationNotFound {
			return lifecycle.Event{}, fault.New(fault.InstallationNotFound, "lifecycle event not found", err)
		}
		return lifecycle.Event{}, err
	}
	return item, nil
}

type lifecycleEventScanner interface {
	Scan(...any) error
}

func scanLifecycleEvent(scanner lifecycleEventScanner) (lifecycle.Event, error) {
	var item lifecycle.Event
	var receivedAt, clientAt, metadataJSON string
	if err := scanner.Scan(&item.Sequence, &item.InstallationID, &item.Stage, &item.Source, &receivedAt, &clientAt, &item.RequestID,
		&item.IdempotencyKey, &item.Message, &item.ErrorCode, &metadataJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return lifecycle.Event{}, fault.New(fault.InstallationNotFound, "lifecycle event not found", err)
		}
		return lifecycle.Event{}, fault.New(fault.StorageFailure, "could not read lifecycle event", err)
	}
	var err error
	item.ReceivedAt, err = time.Parse(time.RFC3339Nano, receivedAt)
	if err != nil {
		return lifecycle.Event{}, fault.New(fault.StorageFailure, "could not parse lifecycle receive time", err)
	}
	if clientAt != "" {
		item.ClientAt, err = time.Parse(time.RFC3339Nano, clientAt)
		if err != nil {
			return lifecycle.Event{}, fault.New(fault.StorageFailure, "could not parse lifecycle client time", err)
		}
	}
	if err := json.Unmarshal([]byte(metadataJSON), &item.Metadata); err != nil {
		return lifecycle.Event{}, fault.New(fault.StorageFailure, "could not decode lifecycle metadata", err)
	}
	if item.Metadata == nil {
		item.Metadata = map[string]string{}
	}
	return item, nil
}

func installationLogByIdempotencyTx(ctx context.Context, tx *sql.Tx, installationID, key string) (telemetry.LogChunk, error) {
	row := tx.QueryRowContext(ctx, `SELECT id,installation_id,sequence,source,received_at,client_at,request_id,idempotency_key,content,content_sha256
		FROM installation_log_chunks WHERE installation_id=? AND idempotency_key=?`, installationID, key)
	item, err := scanInstallationLogChunk(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || fault.Code(err) == fault.InstallationNotFound {
			return telemetry.LogChunk{}, fault.New(fault.InstallationNotFound, "installer log chunk not found", err)
		}
		return telemetry.LogChunk{}, err
	}
	return item, nil
}

type installationLogScanner interface {
	Scan(...any) error
}

func scanInstallationLogChunk(scanner installationLogScanner) (telemetry.LogChunk, error) {
	var item telemetry.LogChunk
	var receivedAt, clientAt string
	if err := scanner.Scan(&item.ID, &item.InstallationID, &item.Sequence, &item.Source, &receivedAt, &clientAt, &item.RequestID,
		&item.IdempotencyKey, &item.Content, &item.Digest); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return telemetry.LogChunk{}, fault.New(fault.InstallationNotFound, "installer log chunk not found", err)
		}
		return telemetry.LogChunk{}, fault.New(fault.StorageFailure, "could not read installer log chunk", err)
	}
	var err error
	item.ReceivedAt, err = time.Parse(time.RFC3339Nano, receivedAt)
	if err != nil {
		return telemetry.LogChunk{}, fault.New(fault.StorageFailure, "could not parse installer log receive time", err)
	}
	if clientAt != "" {
		item.ClientAt, err = time.Parse(time.RFC3339Nano, clientAt)
		if err != nil {
			return telemetry.LogChunk{}, fault.New(fault.StorageFailure, "could not parse installer log client time", err)
		}
	}
	return item, nil
}

func sameOptionalTime(left, right time.Time) bool {
	if left.IsZero() && right.IsZero() {
		return true
	}
	if left.IsZero() != right.IsZero() {
		return false
	}
	return left.UTC().Equal(right.UTC())
}

func cloneMetadata(input map[string]string) map[string]string {
	if input == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
