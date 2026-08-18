package observability

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestLoggerRedactsSecrets(t *testing.T) {
	var out bytes.Buffer
	log := New(&out, slog.LevelInfo)
	log.Info("test", "machine_id", "m_123", "installation_token", "super-secret")

	got := out.String()
	if strings.Contains(got, "super-secret") || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("secret redaction failed: %s", got)
	}
	if !strings.Contains(got, "m_123") {
		t.Fatalf("safe correlation field missing: %s", got)
	}
}
