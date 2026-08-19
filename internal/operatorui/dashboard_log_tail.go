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
	before, err := strconv.ParseUint(strings.TrimSpace(r.URL.Query().Get("before")), 10, 64)
	if err != nil || before == 0 {
		before = h.logs.LatestSequence()
	}
	entries := h.logs.TailThrough(before, dashboardInitialLogTail)
	out := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		out = append(out, map[string]any{"sequence": entry.Sequence, "line": entry.Line})
	}
	writeDashboardJSON(w, http.StatusOK, map[string]any{"entries": out})
}
