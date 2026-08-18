package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStudioListenerRequiresTrustedProxyOutsideLoopback(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8091", "[::1]:8091"} {
		if err := validateStudioListen(address, false); err != nil {
			t.Fatalf("validateStudioListen(%q,false) = %v, want success", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:8091", "192.168.100.10:8091", "[::]:8091"} {
		if err := validateStudioListen(address, false); err == nil {
			t.Fatalf("validateStudioListen(%q,false) succeeded, want fail-closed rejection", address)
		}
		if err := validateStudioListen(address, true); err != nil {
			t.Fatalf("validateStudioListen(%q,true) = %v, want trusted-proxy success", address, err)
		}
	}
	if err := validateStudioListen("localhost:8091", true); err == nil {
		t.Fatal("hostname studio listener should be rejected; explicit IP literals keep the trust boundary unambiguous")
	}
}

func TestPXESurfaceExposesBootButNotStudio(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := pxeSurface(upstream)
	for _, path := range []string{"/healthz", "/boot/discovery.ipxe", "/boot/installations/i_test/boot.ipxe", "/api/v1/discovery.ipxe"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("PXE path %q status=%d want=%d", path, rec.Code, http.StatusNoContent)
		}
	}
	for _, path := range []string{"/", "/ui/", "/ui/operator/", "/api/v1/machines"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("non-PXE path %q status=%d want=404", path, rec.Code)
		}
	}
}

func TestStudioSurfaceHidesBootAndUsesUnifiedDashboardAsRoot(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := studioSurface(upstream)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTemporaryRedirect || rec.Header().Get("Location") != "/ui/" {
		t.Fatalf("studio root status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}

	for _, path := range []string{"/ui/", "/ui/operator/", "/ui/machines/m_test", "/api/v1/machines", "/healthz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("Studio path %q status=%d want=%d", path, rec.Code, http.StatusNoContent)
		}
	}
	for _, path := range []string{"/boot/discovery.ipxe", "/api/v1/discovery.ipxe"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("boot path %q leaked onto Studio listener with status=%d", path, rec.Code)
		}
	}
}

func TestHTTPServerWriteTimeoutCoversFullDebianTrustResolution(t *testing.T) {
	server := newHTTPServer("127.0.0.1:0", http.NotFoundHandler())
	if server.WriteTimeout != 10*time.Minute {
		t.Fatalf("WriteTimeout=%s want=10m", server.WriteTimeout)
	}
}
