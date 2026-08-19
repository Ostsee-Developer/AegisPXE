package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/telemetryauth"
)

const telemetryAuthClockSkew = 5 * time.Minute

func (s *Store) VerifyLifecycleMAC(ctx context.Context, installationID, method, path, idempotencyKey string, unixSeconds int64, body []byte, signature string) error {
	installationID = strings.TrimSpace(installationID)
	if installationID == "" {
		return fault.New(fault.InstallerCredentialRequired, "installation-scoped authentication is required", nil)
	}
	canonical, err := telemetryauth.Canonical(method, path, idempotencyKey, unixSeconds, body)
	if err != nil {
		return fault.New(fault.InstallerCredentialInvalid, "telemetry authentication input is invalid", err)
	}
	requestTime := time.Unix(unixSeconds, 0).UTC()
	now := s.now().UTC()
	if requestTime.Before(now.Add(-telemetryAuthClockSkew)) || requestTime.After(now.Add(telemetryAuthClockSkew)) {
		return fault.New(fault.InstallerCredentialExpired, "telemetry authentication timestamp is outside the accepted window", nil)
	}

	var verifier []byte
	var expiresAt, revokedAt string
	err = s.db.QueryRowContext(ctx, `SELECT secret_sha256,expires_at,revoked_at FROM installation_lifecycle_credentials WHERE installation_id=?`, installationID).Scan(&verifier, &expiresAt, &revokedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fault.New(fault.InstallerCredentialRequired, "installation lifecycle credential is unavailable", err)
		}
		return s.storageError("read lifecycle verifier for telemetry MAC", err)
	}
	if len(verifier) != 32 {
		return fault.New(fault.StorageFailure, "stored lifecycle verifier is invalid", nil)
	}
	expires, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return s.storageError("parse lifecycle credential expiry", err)
	}
	if revokedAt != "" || !expires.After(now) {
		return fault.New(fault.InstallerCredentialExpired, "installation lifecycle credential is expired or revoked", nil)
	}
	if !telemetryauth.Verify(verifier, canonical, signature) {
		return fault.New(fault.InstallerCredentialInvalid, "telemetry request signature is invalid", nil)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE installation_lifecycle_credentials SET last_used_at=? WHERE installation_id=?`, now.Format(time.RFC3339Nano), installationID); err != nil {
		return s.storageError("touch lifecycle credential after telemetry MAC", err)
	}
	return nil
}
