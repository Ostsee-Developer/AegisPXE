package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnsureRequestContextAllocatesAndPropagatesCorrelationID(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	var seenContextID, seenHeaderID string
	handler := ensureRequestContext(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenContextID = requestID(r.Context())
		seenHeaderID = r.Header.Get("X-Request-ID")
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://aegispxe.test/boot/installations/i_test/overlay.cpio", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d", rec.Code)
	}
	if !strings.HasPrefix(seenContextID, "req_") {
		t.Fatalf("context request ID=%q", seenContextID)
	}
	if seenHeaderID != seenContextID {
		t.Fatalf("request header ID=%q context ID=%q", seenHeaderID, seenContextID)
	}
	if got := rec.Header().Get("X-Request-ID"); got != seenContextID {
		t.Fatalf("response header ID=%q context ID=%q", got, seenContextID)
	}
}

func TestEnsureRequestContextPreservesValidCallerID(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	const supplied = "req_external_123"
	var seen string
	handler := ensureRequestContext(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = requestID(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://aegispxe.test/boot/installations/i_test/overlay.cpio", nil)
	req.Header.Set("X-Request-ID", supplied)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if seen != supplied || rec.Header().Get("X-Request-ID") != supplied {
		t.Fatalf("request ID not preserved: context=%q response=%q", seen, rec.Header().Get("X-Request-ID"))
	}
}
