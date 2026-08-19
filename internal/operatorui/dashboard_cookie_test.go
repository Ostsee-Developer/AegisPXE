package operatorui

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/operator"
)

func TestDashboardSessionCookieSecureFlagMatchesBrowserFacingScheme(t *testing.T) {
	handler := &DashboardHandler{}
	session := operator.Session{ExpiresAt: time.Now().UTC().Add(time.Hour)}

	loopback := httptest.NewRequest("GET", "http://127.0.0.1:8091/ui/", nil)
	loopback.RemoteAddr = "127.0.0.1:42000"
	loopbackRecorder := httptest.NewRecorder()
	handler.setDashboardSessionCookie(loopbackRecorder, loopback, "session-token", session)
	loopbackCookie := loopbackRecorder.Header().Get("Set-Cookie")
	if strings.Contains(loopbackCookie, "; Secure") {
		t.Fatalf("plain loopback recovery cookie unexpectedly marked Secure: %s", loopbackCookie)
	}
	if !strings.Contains(loopbackCookie, "HttpOnly") || !strings.Contains(loopbackCookie, "SameSite=Strict") {
		t.Fatalf("plain loopback recovery cookie lost browser protections: %s", loopbackCookie)
	}

	httpsRequest := httptest.NewRequest("GET", "https://pxe.example.test/ui/", nil)
	httpsRecorder := httptest.NewRecorder()
	handler.setDashboardSessionCookie(httpsRecorder, httpsRequest, "session-token", session)
	httpsCookie := httpsRecorder.Header().Get("Set-Cookie")
	if !strings.Contains(httpsCookie, "; Secure") {
		t.Fatalf("HTTPS Studio cookie is not marked Secure: %s", httpsCookie)
	}
}
