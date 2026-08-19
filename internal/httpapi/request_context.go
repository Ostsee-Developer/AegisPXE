package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
)

// ensureRequestContext gives routes registered outside Server.Handler() the
// same correlation contract as the core HTTP surface. It also writes the
// resolved ID back to the request header so the inner requestLog wrapper can
// reuse it instead of allocating a second correlation ID on fallback routes.
func ensureRequestContext(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" || len(requestID) > 128 {
			var err error
			requestID, err = idgen.New("req_")
			if err != nil {
				logger.ErrorContext(r.Context(), "request ID allocation failed",
					"component", "http",
					"operation", "request_id",
					"error_code", fault.StorageFailure,
					"error", err,
				)
				http.Error(w, "internal request tracking failure", http.StatusInternalServerError)
				return
			}
		}

		r.Header.Set("X-Request-ID", requestID)
		w.Header().Set("X-Request-ID", requestID)
		ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
