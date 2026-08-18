package observability

import (
	"log/slog"
	"strings"
	"testing"
)

func TestLogBufferStoresOnlyRedactedLoggerOutput(t *testing.T) {
	buffer := NewLogBuffer(2)
	logger := New(buffer, slog.LevelDebug)
	logger.Info("first", "token", "secret-value", "component", "test")
	logger.Warn("second", "result", "failure")
	logger.Error("third", "recovery_key", "never-store-this")

	entries := buffer.Snapshot(0, 10)
	if len(entries) != 2 {
		t.Fatalf("entries=%d want=2", len(entries))
	}
	joined := entries[0].Line + "\n" + entries[1].Line
	if strings.Contains(joined, "secret-value") || strings.Contains(joined, "never-store-this") {
		t.Fatal("bounded log buffer contains sensitive material")
	}
	if !strings.Contains(joined, "[REDACTED]") || !strings.Contains(joined, `"msg":"third"`) {
		t.Fatalf("unexpected buffered logs: %s", joined)
	}
	if entries[0].Sequence >= entries[1].Sequence {
		t.Fatal("log sequence is not monotonic")
	}
}

func TestLogBufferSnapshotAfterSequence(t *testing.T) {
	buffer := NewLogBuffer(4)
	_, _ = buffer.Write([]byte("one\ntwo\nthree\n"))
	all := buffer.Snapshot(0, 4)
	if len(all) != 3 {
		t.Fatalf("entries=%d want=3", len(all))
	}
	after := buffer.Snapshot(all[0].Sequence, 4)
	if len(after) != 2 || after[0].Line != "two" || after[1].Line != "three" {
		t.Fatalf("unexpected snapshot: %#v", after)
	}
}
