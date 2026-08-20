package agent

import (
	"testing"
	"time"
)

func TestHeartbeatNormalizeRejectsInvalidIdentityFields(t *testing.T) {
	valid := Heartbeat{Version: "0.2.0-dev.1", Generation: 1, BootID: "boot-1", UptimeSeconds: 42, Hostname: "node-1", Kernel: "6.12.0", Architecture: "amd64"}
	if _, err := valid.Normalize(); err != nil {
		t.Fatalf("valid heartbeat rejected: %v", err)
	}
	invalid := []Heartbeat{
		{Version: "", Generation: 1, BootID: "boot-1", Architecture: "amd64"},
		{Version: "0.2.0-dev.1", Generation: 0, BootID: "boot-1", Architecture: "amd64"},
		{Version: "0.2.0-dev.1", Generation: 1, BootID: "", Architecture: "amd64"},
		{Version: "0.2.0-dev.1", Generation: 1, BootID: "boot-1", UptimeSeconds: -1, Architecture: "amd64"},
		{Version: "0.2.0-dev.1", Generation: 1, BootID: "boot-1", Architecture: "mips64"},
	}
	for index, heartbeat := range invalid {
		if _, err := heartbeat.Normalize(); err == nil {
			t.Fatalf("invalid heartbeat %d accepted: %+v", index, heartbeat)
		}
	}
}

func TestProjectPresenceUsesServerReceiptTime(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	record := Record{State: StateOnline, LastSeenAt: now.Add(-30 * time.Second)}
	if got := ProjectPresence(record, now); got != StateOnline {
		t.Fatalf("presence=%q want=%q", got, StateOnline)
	}
	record.LastSeenAt = now.Add(-100 * time.Second)
	if got := ProjectPresence(record, now); got != StateDegraded {
		t.Fatalf("presence=%q want=%q", got, StateDegraded)
	}
	record.LastSeenAt = now.Add(-181 * time.Second)
	if got := ProjectPresence(record, now); got != StateOffline {
		t.Fatalf("presence=%q want=%q", got, StateOffline)
	}
	record.State = StateUnenrolled
	if got := ProjectPresence(record, now); got != StateUnenrolled {
		t.Fatalf("unenrolled presence=%q", got)
	}
}
