package operatorui

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/assignment"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/installation"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
	"github.com/Ostsee-Developer/AegisPXE/internal/operator"
	"github.com/Ostsee-Developer/AegisPXE/internal/operatoridentity"
)

type dashboardInstallationRow struct {
	Spec       installation.Spec
	Machine    machine.Machine
	Assignment *assignment.Assignment
}

type dashboardView struct {
	Page          string
	Title         string
	Description   string
	Session       operator.Session
	Machines      []machine.Machine
	Installations []dashboardInstallationRow
	Users         []operatoridentity.User
	Machine       *machine.Machine
	Identifiers   []machine.Identifier
	Spec          *installation.Spec
	Assignment    *assignment.Assignment
	Wizard        wizardValues
	Error         string
	Known         int
	Provision     int
	Armed         int
	PendingUsers  int
	LogAfter      uint64
}

var dashboardTemplateFuncs = template.FuncMap{
	"upper": strings.ToUpper,
	"timefmt": func(value time.Time) string {
		if value.IsZero() {
			return "—"
		}
		return value.Local().Format("2006-01-02 15:04:05")
	},
	"actor": func(value string) string {
		return strings.TrimPrefix(strings.TrimPrefix(value, "user:"), "recovery:")
	},
}

var dashboardPageTemplate = template.Must(template.New("dashboard").Funcs(dashboardTemplateFuncs).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>AegisPXE · {{.Title}}</title><link rel="stylesheet" href="/ui/assets/dashboard.css"></head><body><div class="app">
<header class="topbar"><a class="brand" href="/ui/"><span class="brand-mark">A</span><span class="brand-copy"><strong>AegisPXE</strong><span>{{.Session.AuthMethod}}</span></span></a><span class="avatar">{{upper (printf "%.2s" (actor .Session.Actor))}}</span></header>
<aside class="sidebar"><a class="brand" href="/ui/"><span class="brand-mark">A</span><span class="brand-copy"><strong>AegisPXE</strong><span>Provisioning control plane</span></span></a><nav><a {{if eq .Page "dashboard"}}class="active"{{end}} href="/ui/">Dashboard</a><a {{if eq .Page "machines"}}class="active"{{end}} href="/ui/machines">Machines</a><a {{if eq .Page "installations"}}class="active"{{end}} href="/ui/installations">Installations</a>{{if .Session.IsAdmin}}<a {{if eq .Page "users"}}class="active"{{end}} href="/ui/users">Users</a>{{end}}<a {{if eq .Page "logs"}}class="active"{{end}} href="/ui/logs">Logs</a></nav><div class="sidebar-foot"><strong>{{actor .Session.Actor}}</strong><small>{{upper (printf "%s" .Session.Role)}} · {{.Session.AuthMethod}}</small><form method="post" action="/ui/logout"><input type="hidden" name="csrf" value="{{.Session.CSRFToken}}"><button class="button ghost small" type="submit">Sign out</button></form></div></aside>
<main class="content"><header class="page-head"><div><p class="eyebrow">AEGISPXE</p><h1>{{.Title}}</h1><p class="muted">{{.Description}}</p></div>{{if eq .Page "installations"}}<div class="actions"><a class="button" href="/ui/installations/new">New installation</a></div>{{end}}</header>
{{if .Error}}<div class="notice danger">{{.Error}}</div>{{end}}
{{if eq .Page "dashboard"}}<section class="stats"><article class="stat"><span>Known machines</span><strong>{{.Known}}</strong></article><article class="stat"><span>Provision approved</span><strong>{{.Provision}}</strong></article><article class="stat"><span>Armed installs</span><strong>{{.Armed}}</strong></article><article class="stat"><span>Pending users</span><strong>{{.PendingUsers}}</strong></article></section><section class="grid two"><article class="card"><div class="card-head"><div><h2>Machines</h2><p class="muted">Discovery inventory and explicit policy.</p></div><a class="button secondary small" href="/ui/machines">Open</a></div>{{if .Machines}}{{range .Machines}}<div class="detail-row"><span>{{.ID}}</span><strong><span class="badge {{.Policy}}">{{upper (printf "%s" .Policy)}}</span></strong></div>{{end}}{{else}}<div class="empty"><p>No machines discovered yet.</p></div>{{end}}</article><article class="card"><div class="card-head"><div><h2>Installations</h2><p class="muted">Immutable specs and assignment state.</p></div><a class="button secondary small" href="/ui/installations">Open</a></div>{{if .Installations}}{{range .Installations}}<div class="detail-row"><span>{{.Spec.Profile.Hostname}}</span><strong><a href="/ui/installations/{{.Spec.ID}}">{{.Spec.ID}}</a></strong></div>{{end}}{{else}}<div class="empty"><p>No InstallationSpecs yet.</p></div>{{end}}</article></section>
{{else if eq .Page "machines"}}{{if .Machine}}<section class="card"><div class="card-head"><div><h2 class="mono">{{.Machine.ID}}</h2><p class="muted">First seen {{timefmt .Machine.FirstSeen}} · Last seen {{timefmt .Machine.LastSeen}}</p></div><span class="badge {{.Machine.Policy}}">{{upper (printf "%s" .Machine.Policy)}}</span></div><dl class="meta"><div><dt>Architecture</dt><dd>{{.Machine.Architecture}}</dd></div><div><dt>Firmware</dt><dd>{{.Machine.Firmware}}</dd></div></dl>{{if .Identifiers}}<div class="section"><div class="section-head"><div><h3>Identifiers</h3><p>Discovery identifiers, never authentication.</p></div></div>{{range .Identifiers}}<div class="detail-row"><span>{{upper (printf "%s" .Kind)}}</span><strong class="mono">{{.Value}}</strong></div>{{end}}</div>{{end}}<div class="card-actions"><form class="inline-form" method="post" action="/ui/machines/{{.Machine.ID}}/policy"><input type="hidden" name="csrf" value="{{.Session.CSRFToken}}"><select name="policy"><option value="pending">Pending</option><option value="local">Local</option><option value="provision">Provision</option><option value="blocked">Blocked</option></select><button class="button" type="submit">Apply policy</button></form></div></section>{{else}}<section class="grid three">{{range .Machines}}<article class="card"><div class="card-head"><div><a class="card-title mono" href="/ui/machines/{{.ID}}">{{.ID}}</a><p class="card-subtitle">{{.Architecture}} · {{.Firmware}}</p></div><span class="badge {{.Policy}}">{{upper (printf "%s" .Policy)}}</span></div><dl class="meta"><div><dt>First seen</dt><dd>{{timefmt .FirstSeen}}</dd></div><div><dt>Last seen</dt><dd>{{timefmt .LastSeen}}</dd></div></dl><div class="card-actions"><form class="inline-form" method="post" action="/ui/machines/{{.ID}}/policy"><input type="hidden" name="csrf" value="{{$.Session.CSRFToken}}"><select name="policy"><option value="pending">Pending</option><option value="local">Local</option><option value="provision">Provision</option><option value="blocked">Blocked</option></select><button class="button small" type="submit">Apply</button></form></div></article>{{else}}<div class="empty"><p>No machines discovered yet.</p></div>{{end}}</section>{{end}}
{{else if eq .Page "installations"}}{{if .Spec}}<section class="card"><div class="card-head"><div><h2 class="mono">{{.Spec.ID}}</h2><p class="muted">Immutable Debian 13 InstallationSpec</p></div>{{if .Assignment}}<span class="badge {{.Assignment.State}}">{{upper (printf "%s" .Assignment.State)}}</span>{{else}}<span class="badge">UNARMED</span>{{end}}</div><div class="detail-list"><div class="detail-row"><span>Machine</span><strong><a href="/ui/machines/{{.Spec.MachineID}}">{{.Spec.MachineID}}</a></strong></div><div class="detail-row"><span>Hostname</span><strong>{{.Spec.Profile.Hostname}}</strong></div><div class="detail-row"><span>Target disk</span><strong class="mono">{{.Spec.Storage.TargetDisk}}</strong></div><div class="detail-row"><span>Driver</span><strong>{{.Spec.DriverID}} v{{.Spec.DriverVersion}}</strong></div><div class="detail-row"><span>Created by</span><strong>{{.Spec.CreatedBy}}</strong></div></div><div class="card-actions">{{if not .Assignment}}<form method="post" action="/ui/installations/{{.Spec.ID}}/arm"><input type="hidden" name="csrf" value="{{.Session.CSRFToken}}"><button class="button" type="submit">Arm next boot</button></form>{{else if eq (printf "%s" .Assignment.State) "armed"}}<form method="post" action="/ui/installations/{{.Spec.ID}}/cancel"><input type="hidden" name="csrf" value="{{.Session.CSRFToken}}"><button class="button secondary" type="submit">Cancel assignment</button></form>{{end}}</div></section>{{else if .Wizard.MachineID}}<form class="form-section" method="post" action="/ui/installations"><input type="hidden" name="csrf" value="{{.Session.CSRFToken}}"><div class="form-grid"><label>Machine<select name="machine_id" required>{{range .Machines}}<option value="{{.ID}}" {{if eq $.Wizard.MachineID .ID}}selected{{end}}>{{.ID}} · {{upper (printf "%s" .Policy)}}</option>{{end}}</select></label><label>Target disk<input name="target_disk" value="{{.Wizard.TargetDisk}}" required></label><label>Hostname<input name="hostname" value="{{.Wizard.Hostname}}" required></label><label>Timezone<input name="timezone" value="{{.Wizard.Timezone}}" required></label><label>Locale<input name="locale" value="{{.Wizard.Locale}}" required></label><label>Keyboard<input name="keyboard" value="{{.Wizard.Keyboard}}" required></label><label>Admin username<input name="admin_username" value="{{.Wizard.AdminUsername}}" required></label><label>Admin full name<input name="admin_full_name" value="{{.Wizard.AdminFullName}}" required></label><label class="wide">SSH public key(s)<textarea name="ssh_keys" required>{{.Wizard.SSHKeys}}</textarea></label><label class="wide">Additional packages<textarea name="packages">{{.Wizard.Packages}}</textarea></label></div><div class="actions"><button class="button" type="submit">Create immutable spec</button><a class="button secondary" href="/ui/installations">Cancel</a></div></form>{{else}}<section class="grid two">{{range .Installations}}<article class="card"><div class="card-head"><div><a class="card-title mono" href="/ui/installations/{{.Spec.ID}}">{{.Spec.ID}}</a><p class="card-subtitle">{{.Spec.Profile.Hostname}} · {{.Spec.Storage.TargetDisk}}</p></div>{{if .Assignment}}<span class="badge {{.Assignment.State}}">{{upper (printf "%s" .Assignment.State)}}</span>{{else}}<span class="badge">UNARMED</span>{{end}}</div><dl class="meta"><div><dt>Machine</dt><dd class="mono">{{.Spec.MachineID}}</dd></div><div><dt>Created</dt><dd>{{timefmt .Spec.CreatedAt}}</dd></div></dl></article>{{else}}<div class="empty"><p>No InstallationSpecs yet.</p></div>{{end}}</section>{{end}}
{{else if eq .Page "users"}}<section class="grid two">{{range .Users}}<article class="card"><div class="card-head"><div><h2>{{.DisplayName}}</h2><p class="card-subtitle mono">{{.Subject}}</p></div><span class="badge {{.Status}}">{{upper (printf "%s" .Status)}}</span></div><dl class="meta"><div><dt>Role</dt><dd>{{if .Role}}{{upper (printf "%s" .Role)}}{{else}}UNASSIGNED{{end}}</dd></div><div><dt>Provider</dt><dd>{{.Provider}}</dd></div></dl>{{if eq (printf "%s" .Status) "pending_review"}}<div class="card-actions"><form method="post" action="/ui/users/{{.ID}}/approve"><input type="hidden" name="csrf" value="{{$.Session.CSRFToken}}"><input type="hidden" name="role" value="operator"><button class="button small" type="submit">Approve operator</button></form><form method="post" action="/ui/users/{{.ID}}/approve"><input type="hidden" name="csrf" value="{{$.Session.CSRFToken}}"><input type="hidden" name="role" value="admin"><button class="button secondary small" type="submit">Approve admin</button></form></div>{{end}}{{if ne (printf "%s" .Status) "blocked"}}<div class="card-actions"><form method="post" action="/ui/users/{{.ID}}/block"><input type="hidden" name="csrf" value="{{$.Session.CSRFToken}}"><button class="button danger small" type="submit">Block</button></form></div>{{end}}</article>{{else}}<div class="empty"><p>No users registered.</p></div>{{end}}</section>
{{else if eq .Page "logs"}}<section class="card"><div class="log-toolbar"><button class="button secondary small" type="button" data-log-pause>Pause</button><button class="button secondary small" type="button" data-log-clear>Clear view</button><a class="button small" href="/ui/logs/export">Save NDJSON</a><span class="log-status" data-log-status>Live</span></div><div class="log-view" data-live-logs data-after="{{.LogAfter}}" aria-live="polite"></div></section>{{end}}
</main><nav class="bottom-nav"><a {{if eq .Page "dashboard"}}class="active"{{end}} href="/ui/"><span class="nav-icon">⌂</span>Home</a><a {{if eq .Page "machines"}}class="active"{{end}} href="/ui/machines"><span class="nav-icon">▣</span>Machines</a><a {{if eq .Page "installations"}}class="active"{{end}} href="/ui/installations"><span class="nav-icon">◇</span>Installs</a>{{if .Session.IsAdmin}}<a {{if eq .Page "users"}}class="active"{{end}} href="/ui/users"><span class="nav-icon">◉</span>Users</a>{{else}}<a href="/ui/logs"><span class="nav-icon">≡</span>Logs</a>{{end}}<a {{if eq .Page "logs"}}class="active"{{end}} href="/ui/logs"><span class="nav-icon">≡</span>Logs</a></nav></div><script src="/ui/assets/dashboard.js" defer></script></body></html>`))

func (h *DashboardHandler) renderDashboardOverview(w http.ResponseWriter, r *http.Request, session operator.Session) {
	machines, err := h.state.Machines(r.Context())
	if err != nil {
		h.writeDashboardError(w, r, "dashboard_machines", err)
		return
	}
	rows, err := h.loadInstallationRows(r)
	if err != nil {
		h.writeDashboardError(w, r, "dashboard_installations", err)
		return
	}
	users, err := h.state.OperatorUsers(r.Context())
	if err != nil {
		h.writeDashboardError(w, r, "dashboard_users", err)
		return
	}
	view := dashboardView{Page: "dashboard", Title: "Dashboard", Description: "Live provisioning state without inferred lifecycle progress.", Session: session, Machines: machines, Installations: rows, Users: users, Known: len(machines)}
	for _, item := range machines {
		if item.Policy == machine.PolicyProvision {
			view.Provision++
		}
	}
	for _, row := range rows {
		if row.Assignment != nil && row.Assignment.State == assignment.StateArmed {
			view.Armed++
		}
	}
	for _, user := range users {
		if user.Status == operatoridentity.StatusPendingReview {
			view.PendingUsers++
		}
	}
	if len(view.Machines) > 4 {
		view.Machines = view.Machines[:4]
	}
	if len(view.Installations) > 4 {
		view.Installations = view.Installations[:4]
	}
	h.renderDashboard(w, view)
}

func (h *DashboardHandler) dashboardUsers(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireDashboardPage(w, r)
	if !ok {
		return
	}
	if !session.IsAdmin() {
		http.Error(w, "administrator role required", http.StatusForbidden)
		return
	}
	users, err := h.state.OperatorUsers(r.Context())
	if err != nil {
		h.writeDashboardError(w, r, "users", err)
		return
	}
	h.renderDashboard(w, dashboardView{Page: "users", Title: "Users", Description: "AegisPXE authorization stays independent from the outer identity provider.", Session: session, Users: users})
}

func (h *DashboardHandler) dashboardApproveUser(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireDashboardAction(w, r, true)
	if !ok {
		return
	}
	role := operatoridentity.Role(strings.TrimSpace(r.PostForm.Get("role")))
	if _, err := h.state.ApproveOperatorUser(r.Context(), strings.TrimSpace(r.PathValue("id")), role, requestID(r), session.Actor); err != nil {
		h.writeDashboardError(w, r, "approve_user", err)
		return
	}
	http.Redirect(w, r, "/ui/users", http.StatusSeeOther)
}

func (h *DashboardHandler) dashboardBlockUser(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireDashboardAction(w, r, true)
	if !ok {
		return
	}
	if _, err := h.state.BlockOperatorUser(r.Context(), strings.TrimSpace(r.PathValue("id")), requestID(r), session.Actor); err != nil {
		h.writeDashboardError(w, r, "block_user", err)
		return
	}
	http.Redirect(w, r, "/ui/users", http.StatusSeeOther)
}

func (h *DashboardHandler) dashboardLogs(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireDashboardPage(w, r)
	if !ok {
		return
	}
	h.renderDashboard(w, dashboardView{Page: "logs", Title: "Logs", Description: "Live redacted structured logs from the same logging pipeline used by journald.", Session: session, LogAfter: h.logs.LatestSequence()})
}

func (h *DashboardHandler) dashboardLogFeed(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireDashboardPageJSON(w, r); !ok {
		return
	}
	after, _ := strconv.ParseUint(strings.TrimSpace(r.URL.Query().Get("after")), 10, 64)
	entries := h.logs.Snapshot(after, 500)
	out := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		out = append(out, map[string]any{"sequence": entry.Sequence, "line": entry.Line})
	}
	writeDashboardJSON(w, http.StatusOK, map[string]any{"entries": out})
}

func (h *DashboardHandler) dashboardLogExport(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireDashboardPage(w, r)
	if !ok {
		return
	}
	entries := h.logs.Snapshot(0, 100000)
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="aegispxe-logs.ndjson"`)
	for _, entry := range entries {
		_, _ = fmt.Fprintln(w, entry.Line)
	}
	h.logger.InfoContext(r.Context(), "operator exported redacted logs", "component", "operator.logs", "operation", "export", "request_id", requestID(r), "actor", session.Actor, "entries", len(entries), "result", "success")
}

func (h *DashboardHandler) requireDashboardPageJSON(w http.ResponseWriter, r *http.Request) (operator.Session, bool) {
	session, _, ok := h.dashboardSession(r)
	if !ok {
		writeDashboardJSON(w, http.StatusUnauthorized, map[string]string{"message": "dashboard session required"})
		return operator.Session{}, false
	}
	return session, true
}

func (h *DashboardHandler) loadInstallationRows(r *http.Request) ([]dashboardInstallationRow, error) {
	specs, err := h.state.InstallationSpecs(r.Context())
	if err != nil {
		return nil, err
	}
	rows := make([]dashboardInstallationRow, 0, len(specs))
	for _, spec := range specs {
		item, err := h.state.Machine(r.Context(), spec.MachineID)
		if err != nil {
			return nil, err
		}
		row := dashboardInstallationRow{Spec: spec, Machine: item}
		stored, err := h.state.AssignmentForInstallation(r.Context(), spec.ID)
		if err == nil {
			row.Assignment = &stored
		} else if fault.Code(err) != fault.InstallationAssignmentNotFound {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (h *DashboardHandler) renderDashboard(w http.ResponseWriter, view dashboardView) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := dashboardPageTemplate.Execute(w, view); err != nil {
		h.logger.Error("dashboard render failed", "component", "operator.http", "operation", "render", "error_code", fault.StorageFailure, "result", "failure", "cause", err.Error())
	}
}

func (h *DashboardHandler) writeDashboardError(w http.ResponseWriter, r *http.Request, operation string, err error) {
	code := fault.Code(err)
	status := http.StatusBadRequest
	switch code {
	case fault.MachineNotFound, fault.InstallationNotFound, fault.InstallationAssignmentNotFound, fault.OperatorUserNotFound:
		status = http.StatusNotFound
	case fault.InstallationAssignmentConflict:
		status = http.StatusConflict
	case fault.OperatorAuthorizationDenied:
		status = http.StatusForbidden
	case fault.StorageFailure:
		status = http.StatusServiceUnavailable
	}
	if code == "" {
		code = fault.StorageFailure
		status = http.StatusInternalServerError
	}
	h.logger.WarnContext(r.Context(), "dashboard operation failed", "component", "operator.http", "operation", operation, "request_id", requestID(r), "error_code", code, "result", "failure", "cause", err.Error())
	http.Error(w, "dashboard operation failed: "+code, status)
}
