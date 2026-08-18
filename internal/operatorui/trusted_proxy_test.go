package operatorui

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/operator"
)

func TestTrustedProxyIssuesSessionOnlyForConfiguredDirectPeer(t *testing.T) {
	proxy, err := ParseTrustedProxy("192.0.2.10/32", "X-Remote-User", "X-Forwarded-Proto")
	if err != nil {
		t.Fatal(err)
	}
	key, err := operator.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	auth, err := operator.New(key, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := NewWithTrustedProxy(next, nil, auth, logger, proxy)

	trusted := httptest.NewRequest(http.MethodGet, "http://studio.test/ui/status", nil)
	trusted.RemoteAddr = "192.0.2.10:41000"
	trusted.Header.Set("X-Forwarded-Proto", "https")
	trusted.Header.Set("X-Remote-User", "alice@example.test")
	trustedRec := httptest.NewRecorder()
	handler.ServeHTTP(trustedRec, trusted)
	cookies := trustedRec.Result().Cookies()
	if trustedRec.Code != http.StatusNoContent || len(cookies) != 1 {
		t.Fatalf("trusted request status=%d cookies=%d", trustedRec.Code, len(cookies))
	}
	session, ok := auth.Session(cookies[0].Value)
	if !ok || session.Actor != "proxy:alice@example.test" {
		t.Fatalf("trusted proxy actor=%q ok=%v", session.Actor, ok)
	}

	spoofed := httptest.NewRequest(http.MethodGet, "http://studio.test/ui/status", nil)
	spoofed.RemoteAddr = "192.0.2.11:41000"
	spoofed.Header.Set("X-Forwarded-Proto", "https")
	spoofed.Header.Set("X-Remote-User", "admin")
	spoofedRec := httptest.NewRecorder()
	handler.ServeHTTP(spoofedRec, spoofed)
	if len(spoofedRec.Result().Cookies()) != 0 {
		t.Fatal("untrusted peer forged proxy headers and received an operator session")
	}
}

func TestTrustedProxyGateAllowsOnlyLoopbackOrAuthenticatedProxy(t *testing.T) {
	proxy, err := ParseTrustedProxy("192.0.2.10", "X-Remote-User", "X-Forwarded-Proto")
	if err != nil {
		t.Fatal(err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := RequireTrustedProxyOrLoopback(next, proxy, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))

	loopback := httptest.NewRequest(http.MethodGet, "http://studio.test/ui/operator/", nil)
	loopback.RemoteAddr = "127.0.0.1:42000"
	loopbackRec := httptest.NewRecorder()
	handler.ServeHTTP(loopbackRec, loopback)
	if loopbackRec.Code != http.StatusNoContent {
		t.Fatalf("loopback status=%d", loopbackRec.Code)
	}

	trusted := httptest.NewRequest(http.MethodGet, "http://studio.test/ui/operator/", nil)
	trusted.RemoteAddr = "192.0.2.10:42000"
	trusted.Header.Set("X-Forwarded-Proto", "https")
	trusted.Header.Set("X-Remote-User", "alice")
	trustedRec := httptest.NewRecorder()
	handler.ServeHTTP(trustedRec, trusted)
	if trustedRec.Code != http.StatusNoContent {
		t.Fatalf("trusted proxy status=%d", trustedRec.Code)
	}

	direct := httptest.NewRequest(http.MethodGet, "http://studio.test/ui/operator/", nil)
	direct.RemoteAddr = "192.0.2.11:42000"
	direct.Header.Set("X-Forwarded-Proto", "https")
	direct.Header.Set("X-Remote-User", "admin")
	directRec := httptest.NewRecorder()
	handler.ServeHTTP(directRec, direct)
	if directRec.Code != http.StatusForbidden {
		t.Fatalf("untrusted direct status=%d want=403", directRec.Code)
	}
}
