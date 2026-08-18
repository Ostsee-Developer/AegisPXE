package operator

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/observability"
	"github.com/Ostsee-Developer/AegisPXE/internal/operatoridentity"
)

func TestRecoveryKeyFileIsPrivateAndValueIsNeverLogged(t *testing.T) {
	var logs bytes.Buffer
	path := filepath.Join(t.TempDir(), "operator.key")
	manager, err := LoadOrCreate(path, observability.New(&logs, slog.LevelDebug))
	if err != nil {
		t.Fatal(err)
	}
	if manager == nil {
		t.Fatal("operator manager is nil")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	key := strings.TrimSpace(string(content))
	if key == "" {
		t.Fatal("generated recovery key is empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("operator key mode=%#o want=0600", info.Mode().Perm())
	}
	if strings.Contains(logs.String(), key) {
		t.Fatal("operator recovery key leaked into logs")
	}
}

func TestUserSessionRequiresCSRFAndExpires(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(key, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	user := operatoridentity.User{ID: "u_test", Subject: "alice", Role: operatoridentity.RoleAdmin, Status: operatoridentity.StatusActive}

	token, session, err := manager.IssueUserSession(user, "zoraxy+passkey")
	if err != nil {
		t.Fatal(err)
	}
	loaded, ok := manager.Session(token)
	if !ok || loaded.Actor != "user:alice" || loaded.UserID != user.ID || loaded.AuthMethod != "zoraxy+passkey" || loaded.ExpiresAt != session.ExpiresAt {
		t.Fatalf("session verification failed: %+v ok=%v", loaded, ok)
	}
	if !manager.ValidateCSRF(loaded, loaded.CSRFToken) || manager.ValidateCSRF(loaded, "wrong") {
		t.Fatal("CSRF validation contract failed")
	}

	now = now.Add(SessionDuration + time.Second)
	if _, ok := manager.Session(token); ok {
		t.Fatal("expired operator session remained valid")
	}
}

func TestRecoveryKeyRateLimitFailsClosed(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(key, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }

	for i := 0; i < maxLoginAttempts; i++ {
		if err := manager.VerifyRecoveryKey("192.0.2.20", "wrong-key"); err == nil || strings.Contains(err.Error(), "rate limit") {
			t.Fatalf("attempt %d did not fail as invalid credential: %v", i+1, err)
		}
	}
	if err := manager.VerifyRecoveryKey("192.0.2.20", key); err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("rate-limited recovery key was accepted or wrong error: %v", err)
	}
}
