package telemetry

import (
	"errors"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/lifecycle"
)

const (
	MaxLogChunkBytes        = 128 << 10
	MaxInstallationLogBytes = 16 << 20
)

type Credential struct {
	ID             string
	InstallationID string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	RevokedAt      time.Time
	LastUsedAt     time.Time
}

type IssuedCredential struct {
	Credential Credential
	Secret     string
}

type LogChunk struct {
	ID             int64
	InstallationID string
	Sequence       int64
	Source         lifecycle.Source
	ReceivedAt     time.Time
	ClientAt       time.Time
	RequestID      string
	IdempotencyKey string
	Content        string
	Digest         string
}

func (c Credential) Active(now time.Time) bool {
	return !c.CreatedAt.IsZero() && c.ExpiresAt.After(now) && c.RevokedAt.IsZero()
}

func (c LogChunk) Validate() error {
	if strings.TrimSpace(c.InstallationID) == "" {
		return errors.New("installation ID is required")
	}
	if c.Sequence <= 0 {
		return errors.New("log chunk sequence must be positive")
	}
	if c.Source != lifecycle.SourceInstaller && c.Source != lifecycle.SourceFinalizer && c.Source != lifecycle.SourceValidator {
		return errors.New("log chunk source is invalid")
	}
	if err := lifecycle.ValidateIdempotencyKey(c.IdempotencyKey); err != nil {
		return err
	}
	if len(c.Content) == 0 {
		return errors.New("log chunk content is required")
	}
	if len(c.Content) > MaxLogChunkBytes {
		return errors.New("log chunk exceeds size limit")
	}
	return nil
}

func RedactLogContent(content string) string {
	content = strings.ReplaceAll(content, "\x00", "")
	lines := strings.Split(content, "\n")
	for index, line := range lines {
		lower := strings.ToLower(line)
		if sensitiveLogLine(lower) {
			lines[index] = "[REDACTED sensitive installer log line]"
		}
	}
	return strings.Join(lines, "\n")
}

func sensitiveLogLine(lower string) bool {
	for _, marker := range []string{
		"authorization:",
		"bearer ",
		"password=",
		"password:",
		"token=",
		"token:",
		"secret=",
		"secret:",
		"cookie=",
		"cookie:",
		"private_key=",
		"private key-----",
		"recovery_key=",
		"recovery key:",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
