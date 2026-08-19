package telemetry

import (
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/lifecycle"
)

func TestLogChunkValidation(t *testing.T) {
	chunk := LogChunk{
		InstallationID: "i_test",
		Sequence:       1,
		Source:         lifecycle.SourceInstaller,
		IdempotencyKey: "log-1",
		Content:        "installer started",
	}
	if err := chunk.Validate(); err != nil {
		t.Fatalf("valid log chunk rejected: %v", err)
	}
	chunk.Sequence = 0
	if err := chunk.Validate(); err == nil {
		t.Fatal("zero log sequence was accepted")
	}
}

func TestRedactLogContentRemovesSensitiveLines(t *testing.T) {
	input := strings.Join([]string{
		"normal installer line",
		"Authorization: Bearer super-secret",
		"token=abcdef",
		"another normal line",
	}, "\n")
	redacted := RedactLogContent(input)
	if strings.Contains(redacted, "super-secret") || strings.Contains(redacted, "abcdef") {
		t.Fatalf("redaction leaked secret material: %q", redacted)
	}
	if !strings.Contains(redacted, "normal installer line") || !strings.Contains(redacted, "another normal line") {
		t.Fatalf("redaction removed unrelated context: %q", redacted)
	}
}
