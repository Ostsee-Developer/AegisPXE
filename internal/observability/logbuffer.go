package observability

import (
	"bytes"
	"sync"
)

type LogEntry struct {
	Sequence uint64
	Line     string
}

type LogBuffer struct {
	mu       sync.Mutex
	capacity int
	entries  []LogEntry
	next     uint64
	pending  []byte
}

func NewLogBuffer(capacity int) *LogBuffer {
	if capacity < 1 {
		capacity = 1
	}
	return &LogBuffer{capacity: capacity, entries: make([]LogEntry, 0, capacity), next: 1}
}

func (b *LogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pending = append(b.pending, p...)
	for {
		idx := bytes.IndexByte(b.pending, '\n')
		if idx < 0 {
			break
		}
		line := bytes.TrimSpace(b.pending[:idx])
		b.pending = append(b.pending[:0], b.pending[idx+1:]...)
		if len(line) == 0 {
			continue
		}
		b.appendLocked(string(append([]byte(nil), line...)))
	}
	return len(p), nil
}

func (b *LogBuffer) Snapshot(after uint64, limit int) []LogEntry {
	if b == nil {
		return nil
	}
	if limit <= 0 || limit > b.capacity {
		limit = b.capacity
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]LogEntry, 0, limit)
	for _, entry := range b.entries {
		if entry.Sequence <= after {
			continue
		}
		out = append(out, entry)
		if len(out) == limit {
			break
		}
	}
	return out
}

func (b *LogBuffer) LatestSequence() uint64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.entries) == 0 {
		return 0
	}
	return b.entries[len(b.entries)-1].Sequence
}

func (b *LogBuffer) appendLocked(line string) {
	entry := LogEntry{Sequence: b.next, Line: line}
	b.next++
	if len(b.entries) < b.capacity {
		b.entries = append(b.entries, entry)
		return
	}
	copy(b.entries, b.entries[1:])
	b.entries[len(b.entries)-1] = entry
}
