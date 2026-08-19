package operator

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/operatoridentity"
)

func TestOperatorSessionStoreIsBoundedAndReclaimsExpiry(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(key, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 19, 16, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	user := operatoridentity.User{ID: "u_test", Subject: "tester", Role: operatoridentity.RoleOperator, Status: operatoridentity.StatusActive}

	for index := 0; index < maxActiveSessions; index++ {
		if _, _, err := manager.IssueUserSession(user, "test+passkey"); err != nil {
			t.Fatalf("session %d: %v", index, err)
		}
	}
	if _, _, err := manager.IssueUserSession(user, "test+passkey"); err == nil {
		t.Fatal("session store accepted an entry beyond its configured bound")
	}

	now = now.Add(SessionDuration + time.Second)
	if _, _, err := manager.IssueUserSession(user, "test+passkey"); err != nil {
		t.Fatalf("expired sessions were not reclaimed: %v", err)
	}
	if len(manager.sessions) != 1 {
		t.Fatalf("active sessions after expiry cleanup=%d want=1", len(manager.sessions))
	}
}
