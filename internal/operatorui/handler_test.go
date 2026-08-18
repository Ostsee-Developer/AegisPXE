package operatorui

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
	"github.com/Ostsee-Developer/AegisPXE/internal/observability"
	"github.com/Ostsee-Developer/AegisPXE/internal/operator"
	"github.com/Ostsee-Developer/AegisPXE/internal/store"
)

func TestCleartextNetworkOperatorLoginFailsClosedWithoutKeyLeak(t *testing.T) {
	key, auth := testAuth(t)
	var logs bytes.Buffer
	logger := observability.New(&logs, slog.LevelDebug)
	handler := New(http.NotFoundHandler(), nil, auth, logger)

	form := url.Values{"key": {key}}
	req := httptest.NewRequest(http.MethodPost, "http://aegispxe.test/ui/operator/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "192.0.2.10:40123"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUpgradeRequired {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	logText := logs.String()
	if !strings.Contains(logText, `"error_code":"SEC006_SECURE_OPERATOR_TRANSPORT_REQUIRED"`) || !strings.Contains(logText, `"result":"rejected"`) {
		t.Fatalf("insecure login rejection was not logged: %s", logText)
	}
	if strings.Contains(logText, key) {
		t.Fatal("operator key leaked into insecure-login logs")
	}
}

func TestHTTPSLoginCreatesHttpOnlyStrictSessionCookie(t *testing.T) {
	key, auth := testAuth(t)
	var logs bytes.Buffer
	logger := observability.New(&logs, slog.LevelDebug)
	handler := New(http.NotFoundHandler(), nil, auth, logger)

	form := url.Values{"key": {key}}
	req := httptest.NewRequest(http.MethodPost, "https://aegispxe.test/ui/operator/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "192.0.2.11:40123"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/ui/" {
		t.Fatalf("status=%d location=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%d want=1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != sessionCookieName || cookie.Value == "" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie contract failed: %+v", cookie)
	}
	if _, ok := auth.Session(cookie.Value); !ok {
		t.Fatal("issued session cookie does not resolve server-side")
	}
	if strings.Contains(logs.String(), key) || strings.Contains(logs.String(), cookie.Value) {
		t.Fatal("operator key or session token leaked into login logs")
	}
}

func TestAuthenticatedPolicyMutationRequiresCSRFAndAuditsActor(t *testing.T) {
	key, auth := testAuth(t)
	var logs bytes.Buffer
	logger := observability.New(&logs, slog.LevelDebug)
	state, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "aegispxe.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	item, _, err := state.DiscoverMachine(context.Background(), machine.Observation{MAC: "BC:24:11:00:70:01"}, "req_discover_operator")
	if err != nil {
		t.Fatal(err)
	}
	handler := New(http.NotFoundHandler(), state, auth, logger)

	cookie, session := loginOperator(t, handler, auth, key)
	badForm := url.Values{"policy": {"provision"}, "csrf": {"wrong"}}
	badReq := httptest.NewRequest(http.MethodPost, "https://aegispxe.test/ui/operator/machines/"+item.ID+"/policy", strings.NewReader(badForm.Encode()))
	badReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	badReq.RemoteAddr = "192.0.2.12:40123"
	badReq.AddCookie(cookie)
	badRec := httptest.NewRecorder()
	handler.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusForbidden {
		t.Fatalf("bad csrf status=%d body=%s", badRec.Code, badRec.Body.String())
	}
	unchanged, err := state.Machine(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Policy != machine.PolicyPending {
		t.Fatalf("bad csrf mutated policy to %s", unchanged.Policy)
	}

	goodForm := url.Values{"policy": {"provision"}, "csrf": {session.CSRFToken}}
	goodReq := httptest.NewRequest(http.MethodPost, "https://aegispxe.test/ui/operator/machines/"+item.ID+"/policy", strings.NewReader(goodForm.Encode()))
	goodReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	goodReq.Header.Set("X-Request-ID", "req_operator_policy")
	goodReq.RemoteAddr = "192.0.2.12:40123"
	goodReq.AddCookie(cookie)
	goodRec := httptest.NewRecorder()
	handler.ServeHTTP(goodRec, goodReq)
	if goodRec.Code != http.StatusSeeOther {
		t.Fatalf("authorized mutation status=%d body=%s", goodRec.Code, goodRec.Body.String())
	}
	updated, err := state.Machine(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Policy != machine.PolicyProvision {
		t.Fatalf("policy=%s want=provision", updated.Policy)
	}
	events, err := state.Events(context.Background(), event.EntityMachine, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[1].Type != event.MachinePolicyChanged || events[1].Actor != "bootstrap:operator" || events[1].RequestID != "req_operator_policy" {
		t.Fatalf("operator mutation audit mismatch: %+v", events)
	}
	logText := logs.String()
	if !strings.Contains(logText, `"error_code":"SEC005_OPERATOR_CSRF_INVALID"`) || !strings.Contains(logText, `"operation":"authorize"`) {
		t.Fatalf("csrf/auth decisions missing from logs: %s", logText)
	}
	if strings.Contains(logText, key) || strings.Contains(logText, cookie.Value) || strings.Contains(logText, session.CSRFToken) {
		t.Fatal("operator credential/session/csrf leaked into logs")
	}
}

func testAuth(t *testing.T) (string, *operator.Manager) {
	t.Helper()
	key, err := operator.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	auth, err := operator.New(key, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return key, auth
}

func loginOperator(t *testing.T, handler http.Handler, auth *operator.Manager, key string) (*http.Cookie, operator.Session) {
	t.Helper()
	form := url.Values{"key": {key}}
	req := httptest.NewRequest(http.MethodPost, "https://aegispxe.test/ui/operator/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "192.0.2.12:40123"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login status=%d body=%s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies=%d want=1", len(cookies))
	}
	session, ok := auth.Session(cookies[0].Value)
	if !ok {
		t.Fatal("login session not found")
	}
	return cookies[0], session
}
