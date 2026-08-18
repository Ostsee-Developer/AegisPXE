package operatorui

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/observability"
	"github.com/Ostsee-Developer/AegisPXE/internal/operator"
	"github.com/Ostsee-Developer/AegisPXE/internal/operatoridentity"
	"github.com/Ostsee-Developer/AegisPXE/internal/store"
	"github.com/go-webauthn/webauthn/webauthn"
)

func TestDashboardSessionInvalidatesBlockedPrincipal(t *testing.T) {
	var logs bytes.Buffer
	logger := observability.New(&logs, slog.LevelDebug)
	state, err := store.Open(context.Background(), t.TempDir()+"/aegispxe.db", logger)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()

	user, _, err := state.ResolveOperatorUser(context.Background(), trustedProxyProvider, "alice", "Alice", "", "req_discover")
	if err != nil {
		t.Fatal(err)
	}
	user, err = state.ApproveOperatorUser(context.Background(), user.ID, operatoridentity.RoleOperator, "req_approve", "user:admin")
	if err != nil {
		t.Fatal(err)
	}
	credential := webauthn.Credential{ID: []byte("credential-test")}
	if err := state.SaveOperatorCredential(context.Background(), user.ID, "dashboard.test", credential, "req_enroll", "user:alice", true); err != nil {
		t.Fatal(err)
	}
	user, err = state.OperatorUser(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}

	key, err := operator.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	auth, err := operator.New(key, logger)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := auth.IssueUserSession(user, "zoraxy+passkey")
	if err != nil {
		t.Fatal(err)
	}
	h := &DashboardHandler{state: state, auth: auth, logger: logger}
	req := httptest.NewRequest(http.MethodGet, "http://dashboard.test/ui/", nil)
	req.RemoteAddr = "127.0.0.1:41000"
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})

	if _, _, ok := h.dashboardSession(req); !ok {
		t.Fatal("active operator session was rejected before blocking")
	}
	if _, err := state.BlockOperatorUser(context.Background(), user.ID, "req_block", "user:admin"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := h.dashboardSession(req); ok {
		t.Fatal("blocked operator retained dashboard session")
	}
	if _, ok := auth.Session(token); ok {
		t.Fatal("blocked operator session remained in the server-side session store")
	}
	if !strings.Contains(logs.String(), `"operation":"session_revalidate"`) || !strings.Contains(logs.String(), `"cause":"principal_state_changed"`) {
		t.Fatalf("session invalidation was not observably logged: %s", logs.String())
	}
}
