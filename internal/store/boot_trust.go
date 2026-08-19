package store

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/assignment"
	"github.com/Ostsee-Developer/AegisPXE/internal/boottrust"
	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
)

const bootTrustChallengeTTL = 2 * time.Minute
const lifecycleCredentialOAEPLabel = "AegisPXE lifecycle credential v1"

func (s *Store) RegisterBootTrustKey(ctx context.Context, installationID, publicKeyPEM, ekFingerprint, requestID string) (boottrust.Key, bool, error) {
	installationID = strings.TrimSpace(installationID)
	publicKeyPEM = strings.TrimSpace(publicKeyPEM)
	ekFingerprint = strings.TrimSpace(ekFingerprint)
	_, fingerprint, err := boottrust.ParseRSAPublicKeyPEM(publicKeyPEM)
	if err != nil {
		return boottrust.Key{}, false, fault.New(fault.BootTrustKeyInvalid, "boot trust key is invalid", err)
	}
	if err := boottrust.ValidateEKFingerprint(ekFingerprint); err != nil {
		return boottrust.Key{}, false, fault.New(fault.BootTrustKeyInvalid, "TPM endorsement fingerprint is invalid", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return boottrust.Key{}, false, s.storageError("begin boot trust enrollment", err)
	}
	defer tx.Rollback()
	machineID, assignmentState, err := trustBindingTx(ctx, tx, installationID)
	if err != nil {
		return boottrust.Key{}, false, err
	}
	if assignmentState == assignment.StateCancelled {
		return boottrust.Key{}, false, fault.New(fault.BootTrustEnrollmentRequired, "cancelled installation cannot enroll boot trust", nil)
	}
	if err := requireProvisionPolicyTx(ctx, tx, machineID); err != nil {
		return boottrust.Key{}, false, err
	}

	now := s.now().UTC()
	existing, err := bootTrustKeyTx(ctx, tx, machineID, fingerprint)
	if err == nil {
		if existing.PublicKeyPEM != publicKeyPEM || (existing.EKFingerprint != "" && ekFingerprint != "" && existing.EKFingerprint != ekFingerprint) {
			return boottrust.Key{}, false, fault.New(fault.BootTrustKeyInvalid, "boot trust fingerprint collision or binding mismatch", nil)
		}
		if existing.State == boottrust.KeyRevoked {
			return boottrust.Key{}, false, fault.New(fault.BootTrustEnrollmentRequired, "boot trust key was revoked", nil)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE machine_boot_trust_keys SET last_seen_at=?, ek_fingerprint=CASE WHEN ek_fingerprint='' THEN ? ELSE ek_fingerprint END WHERE fingerprint=? AND machine_id=?`, now.Format(time.RFC3339Nano), ekFingerprint, fingerprint, machineID); err != nil {
			return boottrust.Key{}, false, s.storageError("touch boot trust enrollment", err)
		}
		if err := tx.Commit(); err != nil {
			return boottrust.Key{}, false, s.storageError("commit boot trust enrollment refresh", err)
		}
		existing.LastSeenAt = now
		if existing.EKFingerprint == "" {
			existing.EKFingerprint = ekFingerprint
		}
		return existing, false, nil
	}
	if fault.Code(err) != fault.BootTrustEnrollmentRequired {
		return boottrust.Key{}, false, err
	}

	var otherMachine string
	err = tx.QueryRowContext(ctx, `SELECT machine_id FROM machine_boot_trust_keys WHERE fingerprint=? LIMIT 1`, fingerprint).Scan(&otherMachine)
	if err == nil && otherMachine != machineID {
		return boottrust.Key{}, false, fault.New(fault.BootTrustKeyInvalid, "boot trust key is already bound to another machine", nil)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return boottrust.Key{}, false, s.storageError("check boot trust key uniqueness", err)
	}

	item := boottrust.Key{Fingerprint: fingerprint, MachineID: machineID, PublicKeyPEM: publicKeyPEM, EKFingerprint: ekFingerprint, State: boottrust.KeyPending, FirstSeenAt: now, LastSeenAt: now}
	if _, err := tx.ExecContext(ctx, `INSERT INTO machine_boot_trust_keys(fingerprint,machine_id,public_key_pem,ek_fingerprint,state,first_seen_at,last_seen_at,approved_at,approved_by,revoked_at,revoked_by) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, item.Fingerprint, item.MachineID, item.PublicKeyPEM, item.EKFingerprint, item.State, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), "", "", "", ""); err != nil {
		return boottrust.Key{}, false, s.storageError("persist boot trust key", err)
	}
	if err := appendEventTx(ctx, tx, event.Event{EntityType: event.EntityInstallation, EntityID: installationID, Type: "BOOT_TRUST_KEY_DISCOVERED", OccurredAt: now, RequestID: requestID, Actor: "boottrust:client", Message: "TPM-bound boot trust key awaits operator approval"}); err != nil {
		return boottrust.Key{}, false, s.storageError("persist boot trust discovery event", err)
	}
	if err := tx.Commit(); err != nil {
		return boottrust.Key{}, false, s.storageError("commit boot trust enrollment", err)
	}
	s.logger.InfoContext(ctx, "boot trust key discovered", "component", "store.boottrust", "operation", "enroll", "request_id", requestID, "installation_id", installationID, "machine_id", machineID, "key_fingerprint", fingerprint, "has_ek_fingerprint", ekFingerprint != "", "result", "pending_approval")
	return item, true, nil
}

func (s *Store) BootTrustKey(ctx context.Context, machineID, fingerprint string) (boottrust.Key, error) {
	row := s.db.QueryRowContext(ctx, `SELECT fingerprint,machine_id,public_key_pem,ek_fingerprint,state,first_seen_at,last_seen_at,approved_at,approved_by,revoked_at,revoked_by FROM machine_boot_trust_keys WHERE machine_id=? AND fingerprint=?`, strings.TrimSpace(machineID), strings.TrimSpace(fingerprint))
	return scanBootTrustKey(row)
}

func (s *Store) BootTrustKeys(ctx context.Context, machineID string) ([]boottrust.Key, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT fingerprint,machine_id,public_key_pem,ek_fingerprint,state,first_seen_at,last_seen_at,approved_at,approved_by,revoked_at,revoked_by FROM machine_boot_trust_keys WHERE machine_id=? ORDER BY first_seen_at DESC`, strings.TrimSpace(machineID))
	if err != nil {
		return nil, s.storageError("list boot trust keys", err)
	}
	defer rows.Close()
	var out []boottrust.Key
	for rows.Next() {
		item, err := scanBootTrustKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, s.storageError("iterate boot trust keys", err)
	}
	return out, nil
}

func (s *Store) ApproveBootTrustKey(ctx context.Context, machineID, fingerprint, requestID, actor string) (boottrust.Key, error) {
	machineID, fingerprint, actor = strings.TrimSpace(machineID), strings.TrimSpace(fingerprint), strings.TrimSpace(actor)
	if machineID == "" || fingerprint == "" || actor == "" {
		return boottrust.Key{}, fault.New(fault.BootTrustKeyInvalid, "machine, fingerprint and actor are required", nil)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return boottrust.Key{}, s.storageError("begin boot trust approval", err)
	}
	defer tx.Rollback()
	item, err := bootTrustKeyTx(ctx, tx, machineID, fingerprint)
	if err != nil {
		return boottrust.Key{}, err
	}
	if item.State == boottrust.KeyRevoked {
		return boottrust.Key{}, fault.New(fault.BootTrustEnrollmentRequired, "revoked boot trust key cannot be approved", nil)
	}
	if item.State == boottrust.KeyApproved {
		return item, nil
	}
	var approved string
	err = tx.QueryRowContext(ctx, `SELECT fingerprint FROM machine_boot_trust_keys WHERE machine_id=? AND state='approved' LIMIT 1`, machineID).Scan(&approved)
	if err == nil && approved != fingerprint {
		return boottrust.Key{}, fault.New(fault.BootTrustEnrollmentRequired, "machine already has a different approved boot trust key", nil)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return boottrust.Key{}, s.storageError("check approved boot trust key", err)
	}
	now := s.now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE machine_boot_trust_keys SET state='approved',approved_at=?,approved_by=? WHERE machine_id=? AND fingerprint=? AND state='pending'`, now.Format(time.RFC3339Nano), actor, machineID, fingerprint); err != nil {
		return boottrust.Key{}, s.storageError("approve boot trust key", err)
	}
	if err := appendEventTx(ctx, tx, event.Event{EntityType: event.EntityMachine, EntityID: machineID, Type: "BOOT_TRUST_KEY_APPROVED", OccurredAt: now, RequestID: requestID, Actor: actor, Message: "TPM-bound boot trust key explicitly approved"}); err != nil {
		return boottrust.Key{}, s.storageError("persist boot trust approval event", err)
	}
	if err := tx.Commit(); err != nil {
		return boottrust.Key{}, s.storageError("commit boot trust approval", err)
	}
	item.State, item.ApprovedAt, item.ApprovedBy = boottrust.KeyApproved, now, actor
	s.logger.InfoContext(ctx, "boot trust key approved", "component", "store.boottrust", "operation", "approve", "request_id", requestID, "machine_id", machineID, "key_fingerprint", fingerprint, "actor", actor, "result", "success")
	return item, nil
}

func (s *Store) RevokeBootTrustKey(ctx context.Context, machineID, fingerprint, requestID, actor string) (boottrust.Key, error) {
	machineID, fingerprint, actor = strings.TrimSpace(machineID), strings.TrimSpace(fingerprint), strings.TrimSpace(actor)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return boottrust.Key{}, s.storageError("begin boot trust revocation", err)
	}
	defer tx.Rollback()
	item, err := bootTrustKeyTx(ctx, tx, machineID, fingerprint)
	if err != nil {
		return boottrust.Key{}, err
	}
	if item.State == boottrust.KeyRevoked {
		return item, nil
	}
	now := s.now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE machine_boot_trust_keys SET state='revoked',revoked_at=?,revoked_by=? WHERE machine_id=? AND fingerprint=?`, now.Format(time.RFC3339Nano), actor, machineID, fingerprint); err != nil {
		return boottrust.Key{}, s.storageError("revoke boot trust key", err)
	}
	if err := appendEventTx(ctx, tx, event.Event{EntityType: event.EntityMachine, EntityID: machineID, Type: "BOOT_TRUST_KEY_REVOKED", OccurredAt: now, RequestID: requestID, Actor: actor, Message: "TPM-bound boot trust key revoked"}); err != nil {
		return boottrust.Key{}, s.storageError("persist boot trust revocation event", err)
	}
	if err := tx.Commit(); err != nil {
		return boottrust.Key{}, s.storageError("commit boot trust revocation", err)
	}
	item.State, item.RevokedAt, item.RevokedBy = boottrust.KeyRevoked, now, actor
	s.logger.InfoContext(ctx, "boot trust key revoked", "component", "store.boottrust", "operation", "revoke", "request_id", requestID, "machine_id", machineID, "key_fingerprint", fingerprint, "actor", actor, "result", "success")
	return item, nil
}

func (s *Store) CreateBootTrustChallenge(ctx context.Context, installationID, fingerprint, requestID string) (boottrust.Challenge, error) {
	installationID, fingerprint = strings.TrimSpace(installationID), strings.TrimSpace(fingerprint)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return boottrust.Challenge{}, s.storageError("begin boot trust challenge", err)
	}
	defer tx.Rollback()
	machineID, assignmentState, err := trustBindingTx(ctx, tx, installationID)
	if err != nil {
		return boottrust.Challenge{}, err
	}
	if assignmentState == assignment.StateCancelled {
		return boottrust.Challenge{}, fault.New(fault.BootTrustEnrollmentRequired, "installation assignment is cancelled", nil)
	}
	if err := requireProvisionPolicyTx(ctx, tx, machineID); err != nil {
		return boottrust.Challenge{}, err
	}
	key, err := bootTrustKeyTx(ctx, tx, machineID, fingerprint)
	if err != nil || key.State != boottrust.KeyApproved {
		return boottrust.Challenge{}, fault.New(fault.BootTrustEnrollmentRequired, "approved boot trust key is required", err)
	}
	id, err := idgen.New("btc_")
	if err != nil {
		return boottrust.Challenge{}, fault.New(fault.StorageFailure, "could not allocate boot trust challenge", err)
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return boottrust.Challenge{}, fault.New(fault.StorageFailure, "could not generate boot trust challenge", err)
	}
	now := s.now().UTC()
	item := boottrust.Challenge{ID: id, InstallationID: installationID, MachineID: machineID, KeyFingerprint: fingerprint, Nonce: nonce, CreatedAt: now, ExpiresAt: now.Add(bootTrustChallengeTTL)}
	if _, err := tx.ExecContext(ctx, `INSERT INTO installation_boot_trust_challenges(id,installation_id,machine_id,key_fingerprint,nonce,created_at,expires_at,used_at,response_ciphertext,credential_expires_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, item.ID, item.InstallationID, item.MachineID, item.KeyFingerprint, item.Nonce, item.CreatedAt.Format(time.RFC3339Nano), item.ExpiresAt.Format(time.RFC3339Nano), "", []byte{}, ""); err != nil {
		return boottrust.Challenge{}, s.storageError("persist boot trust challenge", err)
	}
	if err := tx.Commit(); err != nil {
		return boottrust.Challenge{}, s.storageError("commit boot trust challenge", err)
	}
	s.logger.InfoContext(ctx, "boot trust challenge issued", "component", "store.boottrust", "operation", "challenge", "request_id", requestID, "installation_id", installationID, "machine_id", machineID, "key_fingerprint", fingerprint, "challenge_id", id, "expires_at", item.ExpiresAt, "result", "success")
	return item, nil
}

func (s *Store) CompleteBootTrustChallenge(ctx context.Context, installationID, challengeID string, signature []byte, requestID string) (boottrust.Release, error) {
	installationID, challengeID = strings.TrimSpace(installationID), strings.TrimSpace(challengeID)
	if installationID == "" || challengeID == "" || len(signature) == 0 || len(signature) > 1024 {
		return boottrust.Release{}, fault.New(fault.BootTrustProofInvalid, "boot trust proof is invalid", nil)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return boottrust.Release{}, s.storageError("begin boot trust proof", err)
	}
	defer tx.Rollback()
	challenge, err := bootTrustChallengeTx(ctx, tx, installationID, challengeID)
	if err != nil {
		return boottrust.Release{}, err
	}
	if !challenge.UsedAt.IsZero() {
		if len(challenge.ResponseCipher) == 0 || challenge.CredentialExpiry.IsZero() {
			return boottrust.Release{}, fault.New(fault.BootTrustReplayRejected, "used challenge has no reusable response", nil)
		}
		return boottrust.Release{ChallengeID: challenge.ID, Ciphertext: append([]byte(nil), challenge.ResponseCipher...), Algorithm: "RSA-OAEP-SHA256", CredentialExpiry: challenge.CredentialExpiry, Duplicate: true}, nil
	}
	now := s.now().UTC()
	if !challenge.ExpiresAt.After(now) {
		return boottrust.Release{}, fault.New(fault.BootTrustChallengeExpired, "boot trust challenge expired", nil)
	}
	machineID, assignmentState, err := trustBindingTx(ctx, tx, installationID)
	if err != nil {
		return boottrust.Release{}, err
	}
	if machineID != challenge.MachineID || assignmentState == assignment.StateCancelled {
		return boottrust.Release{}, fault.New(fault.BootTrustProofInvalid, "boot trust challenge binding is no longer valid", nil)
	}
	if err := requireProvisionPolicyTx(ctx, tx, machineID); err != nil {
		return boottrust.Release{}, err
	}
	key, err := bootTrustKeyTx(ctx, tx, machineID, challenge.KeyFingerprint)
	if err != nil || key.State != boottrust.KeyApproved {
		return boottrust.Release{}, fault.New(fault.BootTrustEnrollmentRequired, "approved boot trust key is required", err)
	}
	publicKey, fingerprint, err := boottrust.ParseRSAPublicKeyPEM(key.PublicKeyPEM)
	if err != nil || fingerprint != challenge.KeyFingerprint {
		return boottrust.Release{}, fault.New(fault.BootTrustKeyInvalid, "stored boot trust key is invalid", err)
	}
	canonical, err := boottrust.CanonicalChallenge(challenge)
	if err != nil {
		return boottrust.Release{}, fault.New(fault.BootTrustProofInvalid, "boot trust challenge is invalid", err)
	}
	digest := sha256.Sum256(canonical)
	if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature); err != nil {
		return boottrust.Release{}, fault.New(fault.BootTrustProofInvalid, "boot trust signature did not verify", err)
	}

	var credentialID string
	if err := tx.QueryRowContext(ctx, `SELECT lifecycle_credential_id FROM installation_specs WHERE id=? AND machine_id=?`, installationID, machineID).Scan(&credentialID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return boottrust.Release{}, fault.New(fault.InstallationNotFound, "installation not found", err)
		}
		return boottrust.Release{}, s.storageError("read lifecycle credential identifier for trust release", err)
	}
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT credential_id FROM installation_lifecycle_credentials WHERE installation_id=?`, installationID).Scan(&existing)
	if err == nil {
		return boottrust.Release{}, fault.New(fault.InstallerTelemetryConflict, "installation lifecycle credential already exists and cannot be re-revealed", nil)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return boottrust.Release{}, s.storageError("check lifecycle credential before trust release", err)
	}
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return boottrust.Release{}, fault.New(fault.StorageFailure, "could not generate lifecycle credential", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)
	secretHash := sha256.Sum256([]byte(secret))
	credentialExpiry := now.Add(defaultLifecycleCredentialTTL)
	ciphertext, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, publicKey, []byte(secret), []byte(lifecycleCredentialOAEPLabel))
	if err != nil {
		return boottrust.Release{}, fault.New(fault.BootTrustKeyInvalid, "could not encrypt lifecycle credential to boot trust key", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO installation_lifecycle_credentials(credential_id,installation_id,secret_sha256,created_at,expires_at,revoked_at,last_used_at) VALUES(?,?,?,?,?,?,?)`, credentialID, installationID, secretHash[:], now.Format(time.RFC3339Nano), credentialExpiry.Format(time.RFC3339Nano), "", ""); err != nil {
		return boottrust.Release{}, s.storageError("persist trust-released lifecycle credential", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE installation_boot_trust_challenges SET used_at=?,response_ciphertext=?,credential_expires_at=? WHERE id=? AND used_at=''`, now.Format(time.RFC3339Nano), ciphertext, credentialExpiry.Format(time.RFC3339Nano), challenge.ID); err != nil {
		return boottrust.Release{}, s.storageError("consume boot trust challenge", err)
	}
	if err := appendEventTx(ctx, tx, event.Event{EntityType: event.EntityInstallation, EntityID: installationID, Type: "BOOT_TRUST_VERIFIED", OccurredAt: now, RequestID: requestID, Actor: "boottrust:tpm", Message: "TPM-bound proof verified; encrypted lifecycle credential released"}); err != nil {
		return boottrust.Release{}, s.storageError("persist boot trust verification event", err)
	}
	if err := tx.Commit(); err != nil {
		return boottrust.Release{}, s.storageError("commit boot trust proof", err)
	}
	s.logger.InfoContext(ctx, "TPM-bound boot trust verified", "component", "store.boottrust", "operation", "prove", "request_id", requestID, "installation_id", installationID, "machine_id", machineID, "key_fingerprint", challenge.KeyFingerprint, "challenge_id", challenge.ID, "credential_id", credentialID, "credential_expires_at", credentialExpiry, "result", "success")
	return boottrust.Release{ChallengeID: challenge.ID, Ciphertext: ciphertext, Algorithm: "RSA-OAEP-SHA256", CredentialExpiry: credentialExpiry}, nil
}

func trustBindingTx(ctx context.Context, tx *sql.Tx, installationID string) (string, assignment.State, error) {
	var machineID string
	var state assignment.State
	err := tx.QueryRowContext(ctx, `SELECT s.machine_id,a.state FROM installation_specs s JOIN installation_assignments a ON a.installation_id=s.id WHERE s.id=?`, strings.TrimSpace(installationID)).Scan(&machineID, &state)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", fault.New(fault.InstallationAssignmentNotFound, "installation assignment not found", err)
		}
		return "", "", fault.New(fault.StorageFailure, "could not read installation trust binding", err)
	}
	return machineID, state, nil
}

func requireProvisionPolicyTx(ctx context.Context, tx *sql.Tx, machineID string) error {
	var policy machine.Policy
	if err := tx.QueryRowContext(ctx, `SELECT policy FROM machines WHERE id=?`, machineID).Scan(&policy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fault.New(fault.MachineNotFound, "machine not found", err)
		}
		return fault.New(fault.StorageFailure, "could not read machine policy for boot trust", err)
	}
	if policy != machine.PolicyProvision {
		return fault.New(fault.BootTrustEnrollmentRequired, "machine is not approved for provisioning", nil)
	}
	return nil
}

func bootTrustKeyTx(ctx context.Context, tx *sql.Tx, machineID, fingerprint string) (boottrust.Key, error) {
	row := tx.QueryRowContext(ctx, `SELECT fingerprint,machine_id,public_key_pem,ek_fingerprint,state,first_seen_at,last_seen_at,approved_at,approved_by,revoked_at,revoked_by FROM machine_boot_trust_keys WHERE machine_id=? AND fingerprint=?`, strings.TrimSpace(machineID), strings.TrimSpace(fingerprint))
	return scanBootTrustKey(row)
}

type rowScanner interface{ Scan(dest ...any) error }

func scanBootTrustKey(row rowScanner) (boottrust.Key, error) {
	var item boottrust.Key
	var firstSeen, lastSeen, approvedAt, revokedAt string
	if err := row.Scan(&item.Fingerprint, &item.MachineID, &item.PublicKeyPEM, &item.EKFingerprint, &item.State, &firstSeen, &lastSeen, &approvedAt, &item.ApprovedBy, &revokedAt, &item.RevokedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return boottrust.Key{}, fault.New(fault.BootTrustEnrollmentRequired, "boot trust key not enrolled", err)
		}
		return boottrust.Key{}, fault.New(fault.StorageFailure, "could not read boot trust key", err)
	}
	var err error
	item.FirstSeenAt, err = time.Parse(time.RFC3339Nano, firstSeen)
	if err != nil { return boottrust.Key{}, fault.New(fault.StorageFailure, "could not parse boot trust key first-seen time", err) }
	item.LastSeenAt, err = time.Parse(time.RFC3339Nano, lastSeen)
	if err != nil { return boottrust.Key{}, fault.New(fault.StorageFailure, "could not parse boot trust key last-seen time", err) }
	if approvedAt != "" { item.ApprovedAt, err = time.Parse(time.RFC3339Nano, approvedAt); if err != nil { return boottrust.Key{}, fault.New(fault.StorageFailure, "could not parse boot trust approval time", err) } }
	if revokedAt != "" { item.RevokedAt, err = time.Parse(time.RFC3339Nano, revokedAt); if err != nil { return boottrust.Key{}, fault.New(fault.StorageFailure, "could not parse boot trust revocation time", err) } }
	return item, nil
}

func bootTrustChallengeTx(ctx context.Context, tx *sql.Tx, installationID, challengeID string) (boottrust.Challenge, error) {
	row := tx.QueryRowContext(ctx, `SELECT id,installation_id,machine_id,key_fingerprint,nonce,created_at,expires_at,used_at,response_ciphertext,credential_expires_at FROM installation_boot_trust_challenges WHERE id=? AND installation_id=?`, challengeID, installationID)
	var item boottrust.Challenge
	var createdAt, expiresAt, usedAt, credentialExpiresAt string
	if err := row.Scan(&item.ID, &item.InstallationID, &item.MachineID, &item.KeyFingerprint, &item.Nonce, &createdAt, &expiresAt, &usedAt, &item.ResponseCipher, &credentialExpiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return boottrust.Challenge{}, fault.New(fault.BootTrustProofInvalid, "boot trust challenge not found", err)
		}
		return boottrust.Challenge{}, fault.New(fault.StorageFailure, "could not read boot trust challenge", err)
	}
	var err error
	item.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); if err != nil { return boottrust.Challenge{}, fault.New(fault.StorageFailure, "could not parse boot trust challenge time", err) }
	item.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAt); if err != nil { return boottrust.Challenge{}, fault.New(fault.StorageFailure, "could not parse boot trust challenge expiry", err) }
	if usedAt != "" { item.UsedAt, err = time.Parse(time.RFC3339Nano, usedAt); if err != nil { return boottrust.Challenge{}, fault.New(fault.StorageFailure, "could not parse boot trust challenge use time", err) } }
	if credentialExpiresAt != "" { item.CredentialExpiry, err = time.Parse(time.RFC3339Nano, credentialExpiresAt); if err != nil { return boottrust.Challenge{}, fault.New(fault.StorageFailure, "could not parse boot trust credential expiry", err) } }
	return item, nil
}
