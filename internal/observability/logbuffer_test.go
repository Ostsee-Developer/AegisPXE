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

func TestLogBufferTailReturnsMostRecentEntries(t *testing.T) {
	buffer := NewLogBuffer(4)
	_, _ = buffer.Write([]byte("one\ntwo\nthree\nfour\nfive\n"))
	tail := buffer.Tail(2)
	if len(tail) != 2 || tail[0].Line != "four" || tail[1].Line != "five" {
		t.Fatalf("unexpected tail: %#v", tail)
	}
	if tail[0].Sequence >= tail[1].Sequence {
		t.Fatal("tail sequence is not monotonic")
	}
	all := buffer.Tail(99)
	if len(all) != 4 || all[0].Line != "two" || all[3].Line != "five" {
		t.Fatalf("tail did not honor ring capacity: %#v", all)
	}
}

func TestLogBufferTailThroughExcludesNewerEntries(t *testing.T) {
	buffer := NewLogBuffer(8)
	_, _ = buffer.Write([]byte("one\ntwo\nthree\nfour\nfive\n"))
	all := buffer.Snapshot(0, 8)
	anchor := all[2].Sequence
	anchored := buffer.TailThrough(anchor, 2)
	if len(anchored) != 2 || anchored[0].Line != "two" || anchored[1].Line != "three" {
		t.Fatalf("unexpected anchored tail: %#v", anchored)
	}
	for _, entry := range anchored {
		if entry.Sequence > anchor {
			t.Fatalf("anchored tail leaked newer sequence %d > %d", entry.Sequence, anchor)
		}
	}
}
