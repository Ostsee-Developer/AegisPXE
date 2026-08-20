package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/agent"
	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
)

const (
	DefaultAgentEnrollmentTTL = 24 * time.Hour
	maxAgentEnrollmentTTL     = 7 * 24 * time.Hour
)

func (s *Store) CreateAgentEnrollmentCredential(ctx context.Context, agentID string, ttl time.Duration, requestID, actor string) (agent.EnrollmentCredential, string, error) {
	agentID = strings.TrimSpace(agentID)
	requestID = strings.TrimSpace(requestID)
	actor = strings.TrimSpace(actor)
	if err := agent.ValidateID(agentID); err != nil || actor == "" {
		return agent.EnrollmentCredential{}, "", fault.New(fault.AgentEnrollmentInvalid, "agent and actor are required", err)
	}
	if ttl == 0 {
		ttl = DefaultAgentEnrollmentTTL
	}
	if ttl <= 0 || ttl > maxAgentEnrollmentTTL {
		return agent.EnrollmentCredential{}, "", fault.New(fault.AgentEnrollmentInvalid, "agent enrollment credential TTL is invalid", nil)
	}
	if requestID == "" {
		var err error
		requestID, err = idgen.New("req_")
		if err != nil {
			return agent.EnrollmentCredential{}, "", fault.New(fault.StorageFailure, "could not allocate request identifier", err)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return agent.EnrollmentCredential{}, "", s.storageError("begin agent enrollment credential creation", err)
	}
	defer tx.Rollback()
	record, err := managedAgentByIDTx(ctx, tx, agentID)
	if errors.Is(err, sql.ErrNoRows) {
		return agent.EnrollmentCredential{}, "", fault.New(fault.AgentNotFound, "managed agent not found", err)
	}
	if err != nil {
		return agent.EnrollmentCredential{}, "", s.storageError("read managed agent for enrollment credential", err)
	}
	if record.State != agent.StateReady && record.State != agent.StateUnenrolled {
		return agent.EnrollmentCredential{}, "", fault.New(fault.AgentConflict, "managed agent is not eligible for initial enrollment", nil)
	}
	var activeCredential string
	err = tx.QueryRowContext(ctx, `SELECT credential_id FROM agent_enrollment_credentials WHERE agent_id=? AND consumed_at='' AND revoked_at='' LIMIT 1`, agentID).Scan(&activeCredential)
	if err == nil {
		return agent.EnrollmentCredential{}, "", fault.New(fault.AgentConflict, "managed agent already has an active enrollment credential", nil)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return agent.EnrollmentCredential{}, "", s.storageError("check active agent enrollment credential", err)
	}

	credentialID, err := idgen.New("aec_")
	if err != nil {
		return agent.EnrollmentCredential{}, "", fault.New(fault.StorageFailure, "could not allocate agent enrollment credential identifier", err)
	}
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return agent.EnrollmentCredential{}, "", fault.New(fault.StorageFailure, "could not generate agent enrollment credential", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)
	verifier := sha256.Sum256([]byte(secret))
	now := s.now().UTC()
	item := agent.EnrollmentCredential{ID: credentialID, AgentID: agentID, CreatedAt: now, ExpiresAt: now.Add(ttl)}
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_enrollment_credentials(credential_id,agent_id,secret_sha256,created_at,expires_at,consumed_at,revoked_at) VALUES(?,?,?,?,?,?,?)`, item.ID, item.AgentID, verifier[:], item.CreatedAt.Format(time.RFC3339Nano), item.ExpiresAt.Format(time.RFC3339Nano), "", ""); err != nil {
		return agent.EnrollmentCredential{}, "", s.storageError("persist agent enrollment credential", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agents SET state=?,updated_at=? WHERE id=?`, agent.StateUnenrolled, now.Format(time.RFC3339Nano), agentID); err != nil {
		return agent.EnrollmentCredential{}, "", s.storageError("mark managed agent unenrolled", err)
	}
	if err := appendEventTx(ctx, tx, event.Event{EntityType: event.EntityAgent, EntityID: agentID, Type: event.AgentEnrollmentCredentialCreated, OccurredAt: now, RequestID: requestID, Actor: actor, Message: "single-use managed agent enrollment credential created"}); err != nil {
		return agent.EnrollmentCredential{}, "", s.storageError("persist agent enrollment credential event", err)
	}
	if err := tx.Commit(); err != nil {
		return agent.EnrollmentCredential{}, "", s.storageError("commit agent enrollment credential creation", err)
	}
	s.logger.InfoContext(ctx, "managed agent enrollment credential created", "component", "store.agent_enrollment", "operation", "create_credential", "request_id", requestID, "agent_id", record.ID, "installation_id", record.InstallationID, "machine_id", record.MachineID, "credential_id", item.ID, "expires_at", item.ExpiresAt, "actor", actor, "result", "success")
	return item, secret, nil
}

func (s *Store) CompleteAgentEnrollment(ctx context.Context, agentID, secret string, certificate agent.Certificate, requestID string) (agent.Record, error) {
	agentID = strings.TrimSpace(agentID)
	secret = strings.TrimSpace(secret)
	requestID = strings.TrimSpace(requestID)
	if err := agent.ValidateID(agentID); err != nil || len(secret) < 32 || len(secret) > 128 {
		return agent.Record{}, fault.New(fault.AgentEnrollmentInvalid, "agent enrollment request is invalid", err)
	}
	if err := validateAgentCertificateRecord(certificate, agentID); err != nil {
		return agent.Record{}, fault.New(fault.AgentCertificateInvalid, "agent certificate metadata is invalid", err)
	}
	if requestID == "" {
		var err error
		requestID, err = idgen.New("req_")
		if err != nil {
			return agent.Record{}, fault.New(fault.StorageFailure, "could not allocate request identifier", err)
		}
	}
	verifier := sha256.Sum256([]byte(secret))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return agent.Record{}, s.storageError("begin managed agent enrollment", err)
	}
	defer tx.Rollback()
	record, err := managedAgentByIDTx(ctx, tx, agentID)
	if errors.Is(err, sql.ErrNoRows) {
		return agent.Record{}, fault.New(fault.AgentNotFound, "managed agent not found", err)
	}
	if err != nil {
		return agent.Record{}, s.storageError("read managed agent for enrollment", err)
	}
	if record.State == agent.StateRevoked {
		return agent.Record{}, fault.New(fault.AgentCertificateRevoked, "revoked managed agent cannot enroll", nil)
	}

	var credentialID string
	var storedVerifier []byte
	var expiresAt string
	err = tx.QueryRowContext(ctx, `SELECT credential_id,secret_sha256,expires_at FROM agent_enrollment_credentials WHERE agent_id=? AND consumed_at='' AND revoked_at='' ORDER BY created_at DESC LIMIT 1`, agentID).Scan(&credentialID, &storedVerifier, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		var consumedID string
		replayErr := tx.QueryRowContext(ctx, `SELECT credential_id FROM agent_enrollment_credentials WHERE agent_id=? AND secret_sha256=? AND consumed_at<>'' ORDER BY created_at DESC LIMIT 1`, agentID, verifier[:]).Scan(&consumedID)
		if replayErr == nil {
			return agent.Record{}, fault.New(fault.AgentEnrollmentReplay, "agent enrollment credential was already consumed", nil)
		}
		if !errors.Is(replayErr, sql.ErrNoRows) {
			return agent.Record{}, s.storageError("check consumed agent enrollment credential", replayErr)
		}
		return agent.Record{}, fault.New(fault.AgentEnrollmentInvalid, "agent enrollment credential is invalid", nil)
	}
	if err != nil {
		return agent.Record{}, s.storageError("read agent enrollment credential", err)
	}
	if subtle.ConstantTimeCompare(storedVerifier, verifier[:]) != 1 {
		return agent.Record{}, fault.New(fault.AgentEnrollmentInvalid, "agent enrollment credential is invalid", nil)
	}
	expiry, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return agent.Record{}, s.storageError("parse agent enrollment credential expiry", err)
	}
	now := s.now().UTC()
	if !expiry.After(now) {
		return agent.Record{}, fault.New(fault.AgentEnrollmentExpired, "agent enrollment credential expired", nil)
	}
	certificate.IssuedAt = certificate.IssuedAt.UTC()
	certificate.ExpiresAt = certificate.ExpiresAt.UTC()
	if !certificate.ExpiresAt.After(now) {
		return agent.Record{}, fault.New(fault.AgentCertificateInvalid, "agent certificate is already expired", nil)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_certificates(fingerprint,agent_id,serial,public_key_sha256,issued_at,expires_at,revoked_at,revoked_by) VALUES(?,?,?,?,?,?,?,?)`, certificate.Fingerprint, agentID, certificate.Serial, certificate.PublicKeySHA256, certificate.IssuedAt.Format(time.RFC3339Nano), certificate.ExpiresAt.Format(time.RFC3339Nano), "", ""); err != nil {
		return agent.Record{}, s.storageError("persist managed agent certificate", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_enrollment_credentials SET consumed_at=? WHERE credential_id=? AND consumed_at='' AND revoked_at=''`, now.Format(time.RFC3339Nano), credentialID); err != nil {
		return agent.Record{}, s.storageError("consume managed agent enrollment credential", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agents SET state=?,updated_at=? WHERE id=?`, agent.StateOffline, now.Format(time.RFC3339Nano), agentID); err != nil {
		return agent.Record{}, s.storageError("mark managed agent enrolled", err)
	}
	if err := appendEventTx(ctx, tx, event.Event{EntityType: event.EntityAgent, EntityID: agentID, Type: event.AgentEnrollmentCompleted, OccurredAt: now, RequestID: requestID, Actor: "agent:enrollment", Message: "managed agent enrollment completed"}); err != nil {
		return agent.Record{}, s.storageError("persist managed agent enrollment event", err)
	}
	if err := appendEventTx(ctx, tx, event.Event{EntityType: event.EntityAgent, EntityID: agentID, Type: event.AgentCertificateIssued, OccurredAt: now, RequestID: requestID, Actor: "system:agent-ca", Message: "managed agent client certificate issued"}); err != nil {
		return agent.Record{}, s.storageError("persist managed agent certificate event", err)
	}
	if err := tx.Commit(); err != nil {
		return agent.Record{}, s.storageError("commit managed agent enrollment", err)
	}
	record.State = agent.StateOffline
	record.UpdatedAt = now
	s.logger.InfoContext(ctx, "managed agent enrollment completed", "component", "store.agent_enrollment", "operation", "complete", "request_id", requestID, "agent_id", record.ID, "installation_id", record.InstallationID, "machine_id", record.MachineID, "certificate_fingerprint", certificate.Fingerprint, "certificate_expires_at", certificate.ExpiresAt, "result", "success")
	return record.Clone(), nil
}

func (s *Store) AuthenticateAgentCertificate(ctx context.Context, fingerprint string) (agent.Record, agent.Certificate, error) {
	fingerprint = strings.ToLower(strings.TrimSpace(fingerprint))
	if !validSHA256Hex(fingerprint) {
		return agent.Record{}, agent.Certificate{}, fault.New(fault.AgentCertificateInvalid, "agent certificate fingerprint is invalid", nil)
	}
	certificate, err := scanAgentCertificate(s.db.QueryRowContext(ctx, `SELECT fingerprint,agent_id,serial,public_key_sha256,issued_at,expires_at,revoked_at,revoked_by FROM agent_certificates WHERE fingerprint=?`, fingerprint))
	if errors.Is(err, sql.ErrNoRows) {
		return agent.Record{}, agent.Certificate{}, fault.New(fault.AgentCertificateInvalid, "agent certificate is not enrolled", err)
	}
	if err != nil {
		return agent.Record{}, agent.Certificate{}, s.storageError("read managed agent certificate", err)
	}
	now := s.now().UTC()
	if !certificate.RevokedAt.IsZero() {
		return agent.Record{}, agent.Certificate{}, fault.New(fault.AgentCertificateRevoked, "agent certificate is revoked", nil)
	}
	if !certificate.ExpiresAt.After(now) {
		return agent.Record{}, agent.Certificate{}, fault.New(fault.AgentCertificateInvalid, "agent certificate is expired", nil)
	}
	record, err := s.ManagedAgent(ctx, certificate.AgentID)
	if err != nil {
		return agent.Record{}, agent.Certificate{}, err
	}
	if record.State == agent.StateRevoked {
		return agent.Record{}, agent.Certificate{}, fault.New(fault.AgentCertificateRevoked, "managed agent is revoked", nil)
	}
	return record, certificate, nil
}

func (s *Store) RecordAgentHeartbeat(ctx context.Context, agentID, certificateFingerprint string, heartbeat agent.Heartbeat, requestID string) (agent.Record, error) {
	agentID = strings.TrimSpace(agentID)
	certificateFingerprint = strings.ToLower(strings.TrimSpace(certificateFingerprint))
	requestID = strings.TrimSpace(requestID)
	if err := agent.ValidateID(agentID); err != nil || !validSHA256Hex(certificateFingerprint) {
		return agent.Record{}, fault.New(fault.AgentHeartbeatInvalid, "agent heartbeat identity is invalid", err)
	}
	normalized, err := heartbeat.Normalize()
	if err != nil {
		return agent.Record{}, fault.New(fault.AgentHeartbeatInvalid, "agent heartbeat payload is invalid", err)
	}
	if requestID == "" {
		requestID, err = idgen.New("req_")
		if err != nil {
			return agent.Record{}, fault.New(fault.StorageFailure, "could not allocate request identifier", err)
		}
	}
	payload, err := json.Marshal(normalized)
	if err != nil || len(payload) > 8192 {
		return agent.Record{}, fault.New(fault.AgentHeartbeatInvalid, "agent heartbeat snapshot is invalid", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return agent.Record{}, s.storageError("begin managed agent heartbeat", err)
	}
	defer tx.Rollback()
	certificate, err := scanAgentCertificate(tx.QueryRowContext(ctx, `SELECT fingerprint,agent_id,serial,public_key_sha256,issued_at,expires_at,revoked_at,revoked_by FROM agent_certificates WHERE fingerprint=?`, certificateFingerprint))
	if errors.Is(err, sql.ErrNoRows) || (err == nil && certificate.AgentID != agentID) {
		return agent.Record{}, fault.New(fault.AgentCertificateInvalid, "agent heartbeat certificate is not valid for this agent", err)
	}
	if err != nil {
		return agent.Record{}, s.storageError("read heartbeat agent certificate", err)
	}
	now := s.now().UTC()
	if !certificate.RevokedAt.IsZero() || !certificate.ExpiresAt.After(now) {
		return agent.Record{}, fault.New(fault.AgentCertificateRevoked, "agent heartbeat certificate is revoked or expired", nil)
	}
	record, err := managedAgentByIDTx(ctx, tx, agentID)
	if err != nil {
		return agent.Record{}, s.storageError("read managed agent for heartbeat", err)
	}
	if record.State == agent.StateRevoked {
		return agent.Record{}, fault.New(fault.AgentCertificateRevoked, "revoked managed agent cannot heartbeat", nil)
	}
	build, err := scanAgentBuild(tx.QueryRowContext(ctx, agentBuildSelect+` WHERE agent_id=? AND generation=?`, agentID, normalized.Generation))
	if errors.Is(err, sql.ErrNoRows) {
		return agent.Record{}, fault.New(fault.AgentHeartbeatInvalid, "agent heartbeat generation is unknown", err)
	}
	if err != nil {
		return agent.Record{}, s.storageError("read managed agent build for heartbeat", err)
	}
	if (build.State != agent.BuildStateReady && build.State != agent.BuildStateSuperseded) || build.Version != normalized.Version || build.Architecture != normalized.Architecture || normalized.Generation > record.DesiredGeneration {
		return agent.Record{}, fault.New(fault.AgentHeartbeatInvalid, "agent heartbeat build identity does not match a trusted generation", nil)
	}
	previousPresence := agent.ProjectPresence(record, now)
	updateState := record.UpdateState
	if updateState == agent.UpdateStateConfirming && normalized.Generation == record.DesiredGeneration {
		updateState = agent.UpdateStateSuccess
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agents SET state=?,active_generation=?,active_version=?,last_seen_at=?,last_heartbeat_json=?,update_state=?,updated_at=? WHERE id=?`, agent.StateOnline, normalized.Generation, normalized.Version, now.Format(time.RFC3339Nano), string(payload), updateState, now.Format(time.RFC3339Nano), agentID); err != nil {
		return agent.Record{}, s.storageError("persist managed agent heartbeat", err)
	}
	if previousPresence != agent.StateOnline {
		if err := appendEventTx(ctx, tx, event.Event{EntityType: event.EntityAgent, EntityID: agentID, Type: event.AgentConnected, OccurredAt: now, RequestID: requestID, Actor: "agent:mtls", Message: "managed agent authenticated heartbeat restored presence"}); err != nil {
			return agent.Record{}, s.storageError("persist managed agent connected event", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return agent.Record{}, s.storageError("commit managed agent heartbeat", err)
	}
	record.State = agent.StateOnline
	record.ActiveGeneration = normalized.Generation
	record.ActiveVersion = normalized.Version
	record.LastSeenAt = now
	record.LastHeartbeatJSON = string(payload)
	record.UpdateState = updateState
	record.UpdatedAt = now
	if previousPresence != agent.StateOnline {
		s.logger.InfoContext(ctx, "managed agent connected", "component", "store.agent_heartbeat", "operation", "record", "request_id", requestID, "agent_id", record.ID, "installation_id", record.InstallationID, "machine_id", record.MachineID, "agent_generation", normalized.Generation, "agent_version", normalized.Version, "result", "online")
	} else {
		s.logger.DebugContext(ctx, "managed agent heartbeat accepted", "component", "store.agent_heartbeat", "operation", "record", "request_id", requestID, "agent_id", record.ID, "installation_id", record.InstallationID, "machine_id", record.MachineID, "agent_generation", normalized.Generation, "agent_version", normalized.Version, "result", "success")
	}
	return record.Clone(), nil
}

func scanAgentCertificate(row scanner) (agent.Certificate, error) {
	var certificate agent.Certificate
	var issuedAt, expiresAt, revokedAt string
	if err := row.Scan(&certificate.Fingerprint, &certificate.AgentID, &certificate.Serial, &certificate.PublicKeySHA256, &issuedAt, &expiresAt, &revokedAt, &certificate.RevokedBy); err != nil {
		return agent.Certificate{}, err
	}
	var err error
	if certificate.IssuedAt, err = time.Parse(time.RFC3339Nano, issuedAt); err != nil {
		return agent.Certificate{}, fmt.Errorf("parse agent certificate issue time: %w", err)
	}
	if certificate.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAt); err != nil {
		return agent.Certificate{}, fmt.Errorf("parse agent certificate expiry: %w", err)
	}
	if revokedAt != "" {
		certificate.RevokedAt, err = time.Parse(time.RFC3339Nano, revokedAt)
		if err != nil {
			return agent.Certificate{}, fmt.Errorf("parse agent certificate revocation time: %w", err)
		}
	}
	return certificate, nil
}

func validateAgentCertificateRecord(certificate agent.Certificate, agentID string) error {
	certificate.Fingerprint = strings.ToLower(strings.TrimSpace(certificate.Fingerprint))
	certificate.PublicKeySHA256 = strings.ToLower(strings.TrimSpace(certificate.PublicKeySHA256))
	certificate.Serial = strings.ToLower(strings.TrimSpace(certificate.Serial))
	if certificate.AgentID != agentID || !validSHA256Hex(certificate.Fingerprint) || !validSHA256Hex(certificate.PublicKeySHA256) {
		return errors.New("agent certificate binding or digest is invalid")
	}
	if certificate.Serial == "" || len(certificate.Serial) > 64 {
		return errors.New("agent certificate serial is invalid")
	}
	if serial, ok := new(big.Int).SetString(certificate.Serial, 16); !ok || serial.Sign() <= 0 {
		return errors.New("agent certificate serial is not positive hexadecimal")
	}
	if certificate.IssuedAt.IsZero() || certificate.ExpiresAt.IsZero() || !certificate.ExpiresAt.After(certificate.IssuedAt) || !certificate.RevokedAt.IsZero() || certificate.RevokedBy != "" {
		return errors.New("agent certificate timestamps are invalid")
	}
	return nil
}
