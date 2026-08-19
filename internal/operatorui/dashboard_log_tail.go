package operatorui

import "net/http"

const dashboardInitialLogTail = 200

func (h *DashboardHandler) dashboardLogTail(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireDashboardPageJSON(w, r); !ok {
		return
	}
	entries := h.logs.Tail(dashboardInitialLogTail)
	out := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		out = append(out, map[string]any{"sequence": entry.Sequence, "line": entry.Line})
	}
	writeDashboardJSON(w, http.StatusOK, map[string]any{"entries": out})
}
