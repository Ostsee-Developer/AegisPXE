package operatorui

import (
	"net/http"
	"strconv"
	"strings"
)

const dashboardInitialLogTail = 200

func (h *DashboardHandler) dashboardLogTail(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireDashboardPageJSON(w, r); !ok {
		return
	}
	rawBefore := strings.TrimSpace(r.URL.Query().Get("before"))
	before := h.logs.LatestSequence()
	if rawBefore != "" {
		parsed, err := strconv.ParseUint(rawBefore, 10, 64)
		if err != nil {
			writeDashboardJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid log sequence"})
			return
		}
		before = parsed
	}
	entries := h.logs.TailThrough(before, dashboardInitialLogTail)
	out := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		out = append(out, map[string]any{"sequence": entry.Sequence, "line": entry.Line})
	}
	writeDashboardJSON(w, http.StatusOK, map[string]any{"entries": out})
}
