package operatorui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func (h *DashboardHandler) dashboardStableLogFeed(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireDashboardPageJSON(w, r); !ok {
		return
	}
	rawAfter := strings.TrimSpace(r.URL.Query().Get("after"))
	var after uint64
	if rawAfter != "" {
		parsed, err := strconv.ParseUint(rawAfter, 10, 64)
		if err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "after must be an unsigned sequence number"})
			return
		}
		after = parsed
	}
	entries := h.logs.Snapshot(after, 500)
	out := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		out = append(out, map[string]any{"sequence": entry.Sequence, "line": entry.Line})
	}
	writeDashboardJSON(w, http.StatusOK, map[string]any{"entries": out})
}

func (h *DashboardHandler) dashboardStableLogExport(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireDashboardPage(w, r)
	if !ok {
		return
	}
	entries := h.logs.Snapshot(0, 100000)
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"aegispxe-logs.ndjson\"")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	for _, entry := range entries {
		_, _ = fmt.Fprintln(w, entry.Line)
	}
	h.logger.InfoContext(r.Context(), "operator exported redacted logs", "component", "operator.logs", "operation", "export", "request_id", requestID(r), "actor", session.Actor, "entries", len(entries), "result", "success")
}
