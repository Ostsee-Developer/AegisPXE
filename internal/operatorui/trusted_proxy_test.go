package operatorui

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTrustedProxyInjectsIdentityOnlyForConfiguredDirectPeer(t *testing.T) {
	proxy, err := ParseTrustedProxy("192.0.2.10/32", "X-Remote-User", "X-Forwarded-Proto")
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	var got externalIdentity
	var gotIdentity bool
	var gotTLS bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := externalIdentityFromRequest(r)
		got = got
		gotIdentity = ok
		if ok {
			got = got
		}
		gotTLS = r.TLS != nil
		w.WriteHeader(http.StatusNoContent)
	})
	handler := &trustedProxyIdentityHandler{next: next, logger: logger, proxy: proxy}

	trusted := httptest.NewRequest(http.MethodGet, "http://dashboard.test/ui/", nil)
	trusted.RemoteAddr = "192.0.2.10:41000"
	trusted.Header.Set("X-Forwarded-Proto", "https")
	trusted.Header.Set("X-Remote-User", "alice@example.test")
	trustedRec := httptest.NewRecorder()
	handler.ServeHTTP(trustedRec, trusted)
	if trustedRec.Code != http.StatusNoContent || !gotIdentity || !gotTLS {
		t.Fatalf("trusted identity status=%d identity=%v tls=%v", trustedRec.Code, gotIdentity, gotTLS)
	}
	if len(trustedRec.Result().Cookies()) != 0 {
		t.Fatal("trusted proxy identity unexpectedly created a dashboard session")
	}

	gotIdentity = false
	gotTLS = false
	spoofed := httptest.NewRequest(http.MethodGet, "http://dashboard.test/ui/", nil)
	spoofed.RemoteAddr = "192.0.2.11:41000"
	spoofed.Header.Set("X-Forwarded-Proto", "https")
	spoofed.Header.Set("X-Remote-User", "admin")
	spoofedRec := httptest.NewRecorder()
	handler.ServeHTTP(spoofedRec, spoofed)
	if gotIdentity || gotTLS {
		t.Fatal("untrusted peer forged the trusted proxy identity boundary")
	}
}

func TestTrustedProxyGateAllowsLoopbackOrSecureProxySourceWithoutIdentity(t *testing.T) {
	proxy, err := ParseTrustedProxy("192.0.2.10", "X-Remote-User", "X-Forwarded-Proto")
	if err != nil {
		t.Fatal(err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := RequireTrustedProxyOrLoopback(next, proxy, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))

	loopback := httptest.NewRequest(http.MethodGet, "http://dashboard.test/healthz", nil)
	loopback.RemoteAddr = "127.0.0.1:42000"
	loopbackRec := httptest.NewRecorder()
	handler.ServeHTTP(loopbackRec, loopback)
	if loopbackRec.Code != http.StatusNoContent {
		t.Fatalf("loopback status=%d", loopbackRec.Code)
	}

	trusted := httptest.NewRequest(http.MethodGet, "http://dashboard.test/healthz", nil)
	trusted.RemoteAddr = "192.0.2.10:42000"
	trusted.Header.Set("X-Forwarded-Proto", "https")
	trustedRec := httptest.NewRecorder()
	handler.ServeHTTP(trustedRec, trusted)
	if trustedRec.Code != http.StatusNoContent {
		t.Fatalf("trusted proxy health source status=%d", trustedRec.Code)
	}

	direct := httptest.NewRequest(http.MethodGet, "http://dashboard.test/ui/", nil)
	direct.RemoteAddr = "192.0.2.11:42000"
	direct.Header.Set("X-Forwarded-Proto", "https")
	direct.Header.Set("X-Remote-User", "admin")
	directRec := httptest.NewRecorder()
	handler.ServeHTTP(directRec, direct)
	if directRec.Code != http.StatusForbidden {
		t.Fatalf("untrusted direct status=%d want=403", directRec.Code)
	}
}
