package operatorui

import (
	"html/template"
	"net/http"
	"strings"

	"github.com/Ostsee-Developer/AegisPXE/internal/boottrust"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
)

type bootTrustView struct {
	Session        any
	InstallationID string
	MachineID      string
	Hostname       string
	Keys           []boottrust.Key
}

var bootTrustTemplate = template.Must(template.New("boot-trust").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>AegisPXE · Boot trust</title><link rel="stylesheet" href="/ui/assets/dashboard.css"></head>
<body><main class="management-shell"><section class="management-card">
<header class="page-head"><div><p class="eyebrow">AEGISPXE · TRUST</p><h1>TPM boot trust</h1><p class="muted">Trust protocol inspection. Debian reporter delivery is suspended in dev.21 until a new runtime path passes the real UEFI/vTPM E2E gate.</p></div><div class="actions"><a class="button secondary" href="/ui/installations/{{.InstallationID}}">Back to installation</a></div></header>
<div class="notice warn">No production PXE listener currently exposes reporter enrollment or proof APIs. Existing keys remain visible for audit; do not interpret them as active Debian runtime trust.</div>
<div class="panel-head"><div><h2>{{.Hostname}}</h2><p class="mono">{{.InstallationID}} · {{.MachineID}}</p></div></div>
{{if .Keys}}<div class="table-wrap"><table class="data-table"><thead><tr><th>State</th><th>TPM key fingerprint</th><th>EK hint</th><th>First seen</th><th>Last seen</th><th>Action</th></tr></thead><tbody>
{{range .Keys}}<tr><td><span class="badge {{.State}}">{{.State}}</span></td><td class="mono">{{.Fingerprint}}</td><td class="mono">{{if .EKFingerprint}}{{.EKFingerprint}}{{else}}not supplied{{end}}</td><td>{{.FirstSeenAt}}</td><td>{{.LastSeenAt}}</td><td>{{if eq (printf "%s" .State) "pending"}}<form method="post" action="/ui/installations/{{$.InstallationID}}/trust/{{.Fingerprint}}/approve"><input type="hidden" name="csrf" value="{{$.Session.CSRFToken}}"><button class="button small" type="submit">Approve stored key</button></form>{{else if eq (printf "%s" .State) "approved"}}<form method="post" action="/ui/installations/{{$.InstallationID}}/trust/{{.Fingerprint}}/revoke"><input type="hidden" name="csrf" value="{{$.Session.CSRFToken}}"><button class="button secondary small" type="submit">Revoke</button></form>{{else}}Revoked{{end}}</td></tr>{{end}}
</tbody></table></div>{{else}}<div class="empty"><p>No stored TPM reporter keys for this machine.</p></div>{{end}}
</section></main></body></html>`))

func (h *DashboardHandler) dashboardInstallationTrust(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireDashboardPage(w, r)
	if !ok {
		return
	}
	installationID := strings.TrimSpace(r.PathValue("id"))
	spec, err := h.state.InstallationSpec(r.Context(), installationID)
	if err != nil {
		h.writeDashboardError(w, r, "boot_trust_installation", err)
		return
	}
	keys, err := h.state.BootTrustKeys(r.Context(), spec.MachineID)
	if err != nil {
		h.writeDashboardError(w, r, "boot_trust_keys", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := bootTrustTemplate.Execute(w, bootTrustView{Session: session, InstallationID: spec.ID, MachineID: spec.MachineID, Hostname: spec.Profile.Hostname, Keys: keys}); err != nil {
		h.logger.ErrorContext(r.Context(), "boot trust page render failed", "component", "operator.boottrust", "operation", "render", "request_id", requestID(r), "installation_id", spec.ID, "machine_id", spec.MachineID, "error_code", fault.StorageFailure, "result", "failure", "cause", err.Error())
	}
}

func (h *DashboardHandler) dashboardApproveBootTrustKey(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireDashboardAction(w, r, true)
	if !ok {
		return
	}
	installationID := strings.TrimSpace(r.PathValue("id"))
	fingerprint := strings.TrimSpace(r.PathValue("fingerprint"))
	spec, err := h.state.InstallationSpec(r.Context(), installationID)
	if err != nil {
		h.writeDashboardError(w, r, "boot_trust_approve_installation", err)
		return
	}
	if _, err := h.state.ApproveBootTrustKey(r.Context(), spec.MachineID, fingerprint, requestID(r), session.Actor); err != nil {
		h.writeDashboardError(w, r, "boot_trust_approve", err)
		return
	}
	http.Redirect(w, r, "/ui/installations/"+installationID+"/trust", http.StatusSeeOther)
}

func (h *DashboardHandler) dashboardRevokeBootTrustKey(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireDashboardAction(w, r, true)
	if !ok {
		return
	}
	installationID := strings.TrimSpace(r.PathValue("id"))
	fingerprint := strings.TrimSpace(r.PathValue("fingerprint"))
	spec, err := h.state.InstallationSpec(r.Context(), installationID)
	if err != nil {
		h.writeDashboardError(w, r, "boot_trust_revoke_installation", err)
		return
	}
	if _, err := h.state.RevokeBootTrustKey(r.Context(), spec.MachineID, fingerprint, requestID(r), session.Actor); err != nil {
		h.writeDashboardError(w, r, "boot_trust_revoke", err)
		return
	}
	http.Redirect(w, r, "/ui/installations/"+installationID+"/trust", http.StatusSeeOther)
}
