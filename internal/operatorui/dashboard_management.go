package operatorui

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
)

type machineManagementView struct {
	Session any
	Machine machine.Machine
}

type installationManagementView struct {
	Session        any
	InstallationID string
	MachineID      string
	Hostname       string
}

var machineManagementTemplate = template.Must(template.New("machine-management").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>AegisPXE · Manage machine</title><link rel="stylesheet" href="/ui/assets/dashboard.css"></head><body><main class="management-shell"><section class="management-card"><header class="page-head"><div><p class="eyebrow">AEGISPXE · MACHINE</p><h1>Manage machine</h1><p class="muted">Human-friendly naming, platform-security evidence and deliberate destructive cleanup.</p></div><div class="actions"><a class="button secondary" href="/ui/machines/{{.Machine.ID}}">Back</a></div></header><div class="panel-head"><div><h2>{{if .Machine.Nickname}}{{.Machine.Nickname}}{{else}}Unnamed machine{{end}}</h2><p class="mono">{{.Machine.ID}}</p></div><span class="badge {{.Machine.Policy}}">{{.Machine.Policy}}</span></div><div class="detail-body"><div class="section"><div class="section-head"><div><h3>Secure Boot evidence</h3><p>Observed from the UEFI variables reported by the signed iPXE runtime. This is policy evidence, not TPM remote attestation.</p></div></div><div class="detail-list"><div class="detail-row"><span>Firmware</span><strong>{{if .Machine.Firmware}}{{.Machine.Firmware}}{{else}}unknown{{end}}</strong></div><div class="detail-row"><span>Secure Boot</span><strong><span class="badge {{.Machine.SecureBootState}}">{{.Machine.SecureBootState}}</span></strong></div><div class="detail-row"><span>Observed</span><strong>{{if .Machine.SecureBootObserved}}{{.Machine.SecureBootObserved}}{{else}}not yet observed{{end}}</strong></div></div></div><div class="section"><div class="section-head"><div><h3>Nickname</h3><p>Up to 80 characters. Discovery identity remains unchanged.</p></div></div><form method="post" action="/ui/machines/{{.Machine.ID}}/nickname"><input type="hidden" name="csrf" value="{{.Session.CSRFToken}}"><label>Nickname<input name="nickname" maxlength="80" value="{{.Machine.Nickname}}" placeholder="e.g. Lab-Node-01"></label><div class="card-actions"><button class="button" type="submit">Save nickname</button></div></form></div>{{if .Session.IsAdmin}}<div class="section"><div class="section-head"><div><h3>Delete machine</h3><p>Only possible after every InstallationSpec for this machine has been deleted. Discovery may create it again on the next PXE boot.</p></div></div><form method="post" action="/ui/machines/{{.Machine.ID}}/delete"><input type="hidden" name="csrf" value="{{.Session.CSRFToken}}"><label>Type the machine ID to confirm<input name="confirm" autocomplete="off" value="" placeholder="{{.Machine.ID}}"></label><div class="card-actions"><button class="button danger" type="submit">Delete machine</button></div></form></div>{{end}}</div></section></main></body></html>`))

var installationManagementTemplate = template.Must(template.New("installation-management").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>AegisPXE · Manage installation</title><link rel="stylesheet" href="/ui/assets/dashboard.css"></head><body><main class="management-shell"><section class="management-card"><header class="page-head"><div><p class="eyebrow">AEGISPXE · INSTALLATION</p><h1>Manage installation</h1><p class="muted">Deletion removes the immutable spec and its correlated lifecycle/log/trust runtime history.</p></div><div class="actions"><a class="button secondary" href="/ui/installations/{{.InstallationID}}">Back</a></div></header><div class="panel-head"><div><h2>{{.Hostname}}</h2><p class="mono">{{.InstallationID}} · {{.MachineID}}</p></div></div><div class="detail-body">{{if .Session.IsAdmin}}<div class="section"><div class="section-head"><div><h3>Delete installation</h3><p>An ARMED assignment must be cancelled first. This operation cannot be undone.</p></div></div><form method="post" action="/ui/installations/{{.InstallationID}}/delete"><input type="hidden" name="csrf" value="{{.Session.CSRFToken}}"><label>Type the installation ID to confirm<input name="confirm" autocomplete="off" value="" placeholder="{{.InstallationID}}"></label><div class="card-actions"><button class="button danger" type="submit">Delete installation</button></div></form></div>{{else}}<div class="notice warn">Administrator role is required for deletion.</div>{{end}}</div></section></main></body></html>`))

func (h *DashboardHandler) dashboardMachineMetadata(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireDashboardPage(w, r); !ok {
		return
	}
	items, err := h.state.Machines(r.Context())
	if err != nil {
		h.writeDashboardError(w, r, "machine_metadata", err)
		return
	}
	metadata := make(map[string]map[string]string, len(items))
	for _, item := range items {
		metadata[item.ID] = map[string]string{
			"nickname":          item.Nickname,
			"secure_boot_state": string(item.SecureBootState),
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(map[string]any{"machines": metadata}); err != nil {
		h.logger.WarnContext(r.Context(), "machine metadata response write failed", "component", "operator.machine", "operation", "metadata", "request_id", requestID(r), "error_code", fault.StorageFailure, "result", "response_write_failed")
	}
}

func (h *DashboardHandler) dashboardMachineManagement(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireDashboardPage(w, r)
	if !ok {
		return
	}
	item, err := h.state.Machine(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		h.writeDashboardError(w, r, "machine_management", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := machineManagementTemplate.Execute(w, machineManagementView{Session: session, Machine: item}); err != nil {
		h.logger.ErrorContext(r.Context(), "machine management page render failed", "component", "operator.machine", "operation", "manage", "request_id", requestID(r), "machine_id", item.ID, "error_code", fault.StorageFailure, "result", "failure", "cause", err.Error())
	}
}

func (h *DashboardHandler) dashboardMachineNickname(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireDashboardAction(w, r, false)
	if !ok {
		return
	}
	machineID := strings.TrimSpace(r.PathValue("id"))
	if _, err := h.state.SetMachineNickname(r.Context(), machineID, r.PostForm.Get("nickname"), requestID(r), session.Actor); err != nil {
		h.writeDashboardError(w, r, "machine_nickname", err)
		return
	}
	http.Redirect(w, r, "/ui/machines/"+machineID+"/manage", http.StatusSeeOther)
}

func (h *DashboardHandler) dashboardDeleteMachine(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireDashboardAction(w, r, true)
	if !ok {
		return
	}
	machineID := strings.TrimSpace(r.PathValue("id"))
	if strings.TrimSpace(r.PostForm.Get("confirm")) != machineID {
		http.Error(w, "machine deletion confirmation does not match", http.StatusBadRequest)
		return
	}
	if err := h.state.DeleteMachine(r.Context(), machineID, requestID(r), session.Actor); err != nil {
		if fault.Code(err) == fault.MachineDeleteConflict {
			http.Error(w, "delete this machine's installations first", http.StatusConflict)
			return
		}
		h.writeDashboardError(w, r, "machine_delete", err)
		return
	}
	http.Redirect(w, r, "/ui/machines", http.StatusSeeOther)
}

func (h *DashboardHandler) dashboardInstallationManagement(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireDashboardPage(w, r)
	if !ok {
		return
	}
	installationID := strings.TrimSpace(r.PathValue("id"))
	spec, err := h.state.InstallationSpec(r.Context(), installationID)
	if err != nil {
		h.writeDashboardError(w, r, "installation_management", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := installationManagementTemplate.Execute(w, installationManagementView{Session: session, InstallationID: spec.ID, MachineID: spec.MachineID, Hostname: spec.Profile.Hostname}); err != nil {
		h.logger.ErrorContext(r.Context(), "installation management page render failed", "component", "operator.installation", "operation", "manage", "request_id", requestID(r), "installation_id", spec.ID, "machine_id", spec.MachineID, "error_code", fault.StorageFailure, "result", "failure", "cause", err.Error())
	}
}

func (h *DashboardHandler) dashboardDeleteInstallation(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireDashboardAction(w, r, true)
	if !ok {
		return
	}
	installationID := strings.TrimSpace(r.PathValue("id"))
	if strings.TrimSpace(r.PostForm.Get("confirm")) != installationID {
		http.Error(w, "installation deletion confirmation does not match", http.StatusBadRequest)
		return
	}
	if err := h.state.DeleteInstallation(r.Context(), installationID, requestID(r), session.Actor); err != nil {
		if fault.Code(err) == fault.InstallationDeleteConflict {
			http.Error(w, "cancel the armed assignment before deleting this installation", http.StatusConflict)
			return
		}
		h.writeDashboardError(w, r, "installation_delete", err)
		return
	}
	http.Redirect(w, r, "/ui/installations", http.StatusSeeOther)
}
