package operator

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"strings"
)

// VerifyRecoveryKey verifies the local recovery factor without creating a
// session. AegisPXE only calls this after another trust factor has already
// succeeded, such as initial-admin bootstrap or a recovery passkey ceremony.
func (m *Manager) VerifyRecoveryKey(remote, suppliedKey string) error {
	if m == nil {
		return errors.New("operator manager is unavailable")
	}
	remote = strings.TrimSpace(remote)
	if remote == "" {
		remote = "unknown"
	}
	now := m.now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(now)

	counter := m.attempts[remote]
	if counter.Started.IsZero() || now.Sub(counter.Started) >= loginWindow {
		counter = loginCounter{Started: now}
	}
	if counter.Count >= maxLoginAttempts {
		m.attempts[remote] = counter
		return errors.New("operator recovery rate limit exceeded")
	}
	counter.Count++
	m.attempts[remote] = counter

	suppliedDigest := sha256.Sum256([]byte(strings.TrimSpace(suppliedKey)))
	if subtle.ConstantTimeCompare(suppliedDigest[:], m.keyDigest[:]) != 1 {
		return errors.New("operator recovery key is invalid")
	}
	delete(m.attempts, remote)
	return nil
}
