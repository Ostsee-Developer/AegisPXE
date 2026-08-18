package operatorui

import (
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Ostsee-Developer/AegisPXE/internal/assignment"
	"github.com/Ostsee-Developer/AegisPXE/internal/drivers/debian13"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
	"github.com/Ostsee-Developer/AegisPXE/internal/installation"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
	"github.com/Ostsee-Developer/AegisPXE/internal/operator"
	"github.com/Ostsee-Developer/AegisPXE/internal/profile"
)

const (
	builtinProfileID       = "builtin:debian13-standard"
	builtinProfileRevision = "1"
)

type debianArtifactResolver interface {
	Resolve(context.Context) (debian13.Resolution, error)
}

type consoleInstallationRow struct {
	Spec       installation.Spec
	Machine    machine.Machine
	Assignment *assignment.Assignment
}

type consoleData struct {
	Session       operator.Session
	Machines      []machine.Machine
	Installations []consoleInstallationRow
	Provision     int
	Armed         int
}

type wizardValues struct {
	MachineID     string
	Hostname      string
	Locale        string
	Keyboard      string
	Timezone      string
	AdminUsername string
	AdminFullName string
	SSHKeys       string
	Packages      string
	TargetDisk    string
}

type wizardData struct {
	Session  operator.Session
	Machines []machine.Machine
	Values   wizardValues
	Error    string
}

var consoleTemplateFuncs = template.FuncMap{
	"timefmt": func(value time.Time) string {
		if value.IsZero() {
			return "—"
		}
		return value.Local().Format("2006-01-02 15:04:05")
	},
	"upper": strings.ToUpper,
}

var operatorConsoleTemplate = template.Must(template.New("operator-console").Funcs(consoleTemplateFuncs).Parse(operatorConsolePage))
var installationWizardTemplate = template.Must(template.New("installation-wizard").Funcs(consoleTemplateFuncs).Parse(installationWizardPage))

func newDebianArtifactResolver(logger *slog.Logger) debianArtifactResolver {
	return debian13.NewArtifactResolver(logger)
}

func (h *Handler) registerConsole() {
	h.mux.HandleFunc("GET /ui/operator/{$}", h.consolePage)
	h.mux.HandleFunc("GET /ui/operator/assets/operator.css", h.operatorStyle)
	h.mux.HandleFunc("GET /ui/operator/installations/new", h.installationWizardPage)
	h.mux.HandleFunc("POST /ui/operator/installations", h.createInstallationFromWizard)
}

func (h *Handler) consolePage(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requirePageSession(w, r)
	if !ok {
		return
	}
	machines, err := h.state.Machines(r.Context())
	if err != nil {
		h.writeMutationError(w, r, "console_machines", err)
		return
	}
	specs, err := h.state.InstallationSpecs(r.Context())
	if err != nil {
		h.writeMutationError(w, r, "console_installations", err)
		return
	}

	data := consoleData{
		Session:       session,
		Machines:      machines,
		Installations: make([]consoleInstallationRow, 0, len(specs)),
	}
	for _, item := range machines {
		if item.Policy == machine.PolicyProvision {
			data.Provision++
		}
	}
	for _, spec := range specs {
		item, err := h.state.Machine(r.Context(), spec.MachineID)
		if err != nil {
			h.writeMutationError(w, r, "console_installation_machine", err)
			return
		}
		row := consoleInstallationRow{Spec: spec, Machine: item}
		stored, err := h.state.AssignmentForInstallation(r.Context(), spec.ID)
		if err == nil {
			row.Assignment = &stored
			if stored.State == assignment.StateArmed {
				data.Armed++
			}
		} else if fault.Code(err) != fault.InstallationAssignmentNotFound {
			h.writeMutationError(w, r, "console_installation_assignment", err)
			return
		}
		data.Installations = append(data.Installations, row)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := operatorConsoleTemplate.Execute(w, data); err != nil {
		h.logger.ErrorContext(r.Context(), "operator console render failed",
			"component", "operator.http",
			"operation", "render_console",
			"request_id", requestID(r),
			"actor", session.Actor,
			"error_code", fault.StorageFailure,
			"result", "failure",
			"cause", err.Error(),
		)
	}
}

func (h *Handler) installationWizardPage(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requirePageSession(w, r)
	if !ok {
		return
	}
	values := defaultWizardValues()
	values.MachineID = strings.TrimSpace(r.URL.Query().Get("machine_id"))
	h.renderWizard(w, r, session, values, "")
}

func (h *Handler) createInstallationFromWizard(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	session, _, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	values := wizardValuesFromRequest(r)

	item, err := h.state.Machine(r.Context(), values.MachineID)
	if err != nil {
		h.logWizardRejected(r, session, values.MachineID, fault.Code(err), "machine_lookup_failed", started)
		h.renderWizard(w, r, session, values, "Selected machine could not be loaded.")
		return
	}
	if item.Policy != machine.PolicyProvision {
		h.logWizardRejected(r, session, item.ID, fault.InstallationSpecInvalid, "machine_not_provision_approved", started)
		h.renderWizard(w, r, session, values, "Machine must be approved with policy PROVISION before creating an installation.")
		return
	}
	if item.Architecture != "" && item.Architecture != "x86_64" && item.Architecture != "amd64" {
		h.logWizardRejected(r, session, item.ID, fault.InstallationSpecInvalid, "machine_architecture_unsupported", started)
		h.renderWizard(w, r, session, values, "Debian 13 Standard currently supports amd64/x86_64 machines only.")
		return
	}

	profileSnapshot := profile.Snapshot{
		SchemaVersion: profile.SchemaVersion,
		Hostname:      strings.TrimSpace(values.Hostname),
		Locale:        strings.TrimSpace(values.Locale),
		Keyboard:      strings.TrimSpace(values.Keyboard),
		Timezone:      strings.TrimSpace(values.Timezone),
		Admin: profile.Admin{
			Username:          strings.TrimSpace(values.AdminUsername),
			FullName:          strings.TrimSpace(values.AdminFullName),
			AuthorizedSSHKeys: parseSSHKeys(values.SSHKeys),
			PasswordlessSudo:  true,
		},
		Packages: parsePackages(values.Packages),
	}
	if err := profileSnapshot.Validate(); err != nil {
		h.logWizardRejected(r, session, item.ID, fault.InstallationSpecInvalid, "profile_validation_failed", started)
		h.renderWizard(w, r, session, values, "Profile input is invalid: "+err.Error())
		return
	}
	if err := installation.ValidateTargetDisk(strings.TrimSpace(values.TargetDisk)); err != nil {
		h.logWizardRejected(r, session, item.ID, fault.InstallationSpecInvalid, "target_disk_invalid", started)
		h.renderWizard(w, r, session, values, "Target disk is invalid. Use a whole device such as /dev/vda, /dev/sda or /dev/nvme0n1.")
		return
	}

	resolveStarted := time.Now()
	resolution, err := h.resolver.Resolve(r.Context())
	if err != nil {
		code := fault.Code(err)
		if code == "" {
			code = fault.ArtifactTrustFailed
		}
		h.logger.WarnContext(r.Context(), "operator Debian artifact resolution failed",
			"component", "operator.installation",
			"operation", "resolve_artifacts",
			"request_id", requestID(r),
			"machine_id", item.ID,
			"actor", session.Actor,
			"error_code", code,
			"result", "failure",
			"cause", err.Error(),
			"duration_ms", time.Since(resolveStarted).Milliseconds(),
		)
		h.renderWizard(w, r, session, values, "Debian installer artifacts could not be verified. Check the server log for "+code+".")
		return
	}
	h.logger.InfoContext(r.Context(), "operator Debian artifacts resolved",
		"component", "operator.installation",
		"operation", "resolve_artifacts",
		"request_id", requestID(r),
		"machine_id", item.ID,
		"actor", session.Actor,
		"release_version", resolution.ReleaseVersion,
		"installer_version", resolution.InstallerVersion,
		"kernel_digest", resolution.Kernel.Descriptor.Digest,
		"initrd_digest", resolution.Initrd.Descriptor.Digest,
		"result", "success",
		"duration_ms", time.Since(resolveStarted).Milliseconds(),
	)

	credentialID, err := idgen.New("lc_")
	if err != nil {
		h.logWizardRejected(r, session, item.ID, fault.StorageFailure, "credential_id_allocation_failed", started)
		h.renderWizard(w, r, session, values, "Could not allocate installation identity metadata.")
		return
	}
	spec := installation.Spec{
		MachineID:       item.ID,
		DriverID:        debian13.DriverID,
		DriverVersion:   debian13.DriverVersion,
		OSRelease:       "13",
		Architecture:    "amd64",
		ProfileID:       builtinProfileID,
		ProfileRevision: builtinProfileRevision,
		Profile:         profileSnapshot,
		Artifacts: []installation.Artifact{
			resolution.Kernel.Descriptor,
			resolution.Initrd.Descriptor,
		},
		Storage: installation.Storage{
			Mode:       "whole-disk",
			Filesystem: "ext4",
			TargetDisk: strings.TrimSpace(values.TargetDisk),
			Encrypted:  false,
			TPM2:       false,
		},
		Security: installation.Security{
			SSHPasswordAuthentication: false,
			RootLogin:                 false,
			AutomaticSecurityUpdates:  true,
		},
		LifecycleCredentialID: credentialID,
		CreatedBy:             session.Actor,
	}
	if err := debian13.ValidateSpec(spec); err != nil {
		h.logWizardRejected(r, session, item.ID, fault.DriverSpecUnsupported, "driver_validation_failed", started)
		h.renderWizard(w, r, session, values, "Debian 13 Standard rejected the requested installation: "+err.Error())
		return
	}
	created, err := h.state.CreateInstallationSpec(r.Context(), spec, requestID(r))
	if err != nil {
		code := fault.Code(err)
		if code == "" {
			code = fault.StorageFailure
		}
		h.logWizardRejected(r, session, item.ID, code, "spec_persistence_failed", started)
		h.renderWizard(w, r, session, values, "InstallationSpec could not be created. Check the server log for "+code+".")
		return
	}

	h.logger.InfoContext(r.Context(), "operator created immutable installation spec",
		"component", "operator.installation",
		"operation", "create_spec",
		"request_id", requestID(r),
		"machine_id", item.ID,
		"installation_id", created.ID,
		"actor", session.Actor,
		"driver_id", created.DriverID,
		"driver_version", created.DriverVersion,
		"release_version", resolution.ReleaseVersion,
		"installer_version", resolution.InstallerVersion,
		"target_disk", created.Storage.TargetDisk,
		"result", "success",
		"duration_ms", time.Since(started).Milliseconds(),
	)
	http.Redirect(w, r, "/ui/installations/"+created.ID, http.StatusSeeOther)
}

func (h *Handler) renderWizard(w http.ResponseWriter, r *http.Request, session operator.Session, values wizardValues, message string) {
	machines, err := h.state.Machines(r.Context())
	if err != nil {
		h.writeMutationError(w, r, "wizard_machines", err)
		return
	}
	provisionMachines := make([]machine.Machine, 0, len(machines))
	for _, item := range machines {
		if item.Policy == machine.PolicyProvision {
			provisionMachines = append(provisionMachines, item)
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := installationWizardTemplate.Execute(w, wizardData{
		Session:  session,
		Machines: provisionMachines,
		Values:   values,
		Error:    message,
	}); err != nil {
		h.logger.ErrorContext(r.Context(), "installation wizard render failed",
			"component", "operator.http",
			"operation", "render_installation_wizard",
			"request_id", requestID(r),
			"actor", session.Actor,
			"error_code", fault.StorageFailure,
			"result", "failure",
			"cause", err.Error(),
		)
	}
}

func (h *Handler) requirePageSession(w http.ResponseWriter, r *http.Request) (operator.Session, bool) {
	if h.auth == nil {
		http.NotFound(w, r)
		return operator.Session{}, false
	}
	if !secureTransport(r) {
		h.logRejected(r, "authorize_page", fault.OperatorSecureTransportRequired, "insecure_transport")
		http.Error(w, "operator console requires secure transport", http.StatusUpgradeRequired)
		return operator.Session{}, false
	}
	session, ok := h.session(r)
	if !ok {
		http.Redirect(w, r, "/ui/operator/login", http.StatusSeeOther)
		return operator.Session{}, false
	}
	return session, true
}

func (h *Handler) logWizardRejected(r *http.Request, session operator.Session, machineID, code, cause string, started time.Time) {
	if code == "" {
		code = fault.InstallationSpecInvalid
	}
	h.logger.WarnContext(r.Context(), "operator installation wizard rejected",
		"component", "operator.installation",
		"operation", "create_spec",
		"request_id", requestID(r),
		"machine_id", machineID,
		"actor", session.Actor,
		"error_code", code,
		"result", "rejected",
		"cause", cause,
		"duration_ms", time.Since(started).Milliseconds(),
	)
}

func defaultWizardValues() wizardValues {
	return wizardValues{
		Hostname:      "debian13",
		Locale:        "de_DE.UTF-8",
		Keyboard:      "de",
		Timezone:      "Europe/Berlin",
		AdminUsername: "guardian",
		AdminFullName: "Aegis Administrator",
		TargetDisk:    "/dev/vda",
	}
}

func wizardValuesFromRequest(r *http.Request) wizardValues {
	return wizardValues{
		MachineID:     strings.TrimSpace(r.PostForm.Get("machine_id")),
		Hostname:      r.PostForm.Get("hostname"),
		Locale:        r.PostForm.Get("locale"),
		Keyboard:      r.PostForm.Get("keyboard"),
		Timezone:      r.PostForm.Get("timezone"),
		AdminUsername: r.PostForm.Get("admin_username"),
		AdminFullName: r.PostForm.Get("admin_full_name"),
		SSHKeys:       r.PostForm.Get("ssh_keys"),
		Packages:      r.PostForm.Get("packages"),
		TargetDisk:    r.PostForm.Get("target_disk"),
	}
}

func parseSSHKeys(value string) []string {
	var keys []string
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			keys = append(keys, line)
		}
	}
	return keys
}

func parsePackages(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == ';'
	})
	seen := make(map[string]struct{}, len(fields))
	packages := make([]string, 0, len(fields))
	for _, field := range fields {
		name := strings.TrimSpace(field)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		packages = append(packages, name)
	}
	sort.Strings(packages)
	return packages
}

func (h *Handler) operatorStyle(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(operatorConsoleCSS))
}

const operatorConsolePage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta http-equiv="refresh" content="10"><title>AegisPXE · Operator Console</title><link rel="stylesheet" href="/ui/assets/style.css"><link rel="stylesheet" href="/ui/operator/assets/operator.css"></head>
<body><div class="shell"><aside class="sidebar"><a class="brand" href="/ui/operator/"><span class="brand-mark">A</span><span><strong>AegisPXE</strong><small>Operator Console</small></span></a><nav><a href="/ui/">Read-only Studio</a><a href="/ui/installations">Installations</a><a class="active" href="/ui/operator/">Operator</a><a href="/ui/operator/installations/new">New installation</a></nav><div class="side-foot"><span class="dot"></span> Authenticated<small>{{.Session.Actor}}</small></div></aside>
<main><header><div><p class="eyebrow">AUTHENTICATED OPERATOR BOUNDARY</p><h1>Operator Console</h1><p class="muted">Explicit provisioning mutations. Every accepted change is auditable.</p></div><form method="post" action="/ui/operator/logout"><input type="hidden" name="csrf" value="{{.Session.CSRFToken}}"><button class="op-button op-secondary" type="submit">Sign out</button></form></header>
<section class="stats stats-four"><article><span>Known machines</span><strong>{{len .Machines}}</strong></article><article><span>Provision approved</span><strong>{{.Provision}}</strong></article><article><span>Installation specs</span><strong>{{len .Installations}}</strong></article><article><span>Armed</span><strong>{{.Armed}}</strong></article></section>
<section class="panel op-section"><div class="panel-head"><div><h2>Machines</h2><p>Policy is operator intent, not machine authentication.</p></div><a class="op-button" href="/ui/">Open discovery inventory</a></div>{{if .Machines}}<div class="table-wrap"><table><thead><tr><th>Machine</th><th>Architecture</th><th>Firmware</th><th>Policy</th><th>Operator action</th></tr></thead><tbody>{{range .Machines}}<tr><td><a class="machine-link" href="/ui/machines/{{.ID}}">{{.ID}}</a></td><td>{{.Architecture}}</td><td>{{.Firmware}}</td><td><span class="badge badge-{{.Policy}}">{{upper (printf "%s" .Policy)}}</span></td><td><form class="op-inline" method="post" action="/ui/operator/machines/{{.ID}}/policy"><input type="hidden" name="csrf" value="{{$.Session.CSRFToken}}"><select name="policy" aria-label="Machine policy"><option value="pending" {{if eq (printf "%s" .Policy) "pending"}}selected{{end}}>Pending</option><option value="local" {{if eq (printf "%s" .Policy) "local"}}selected{{end}}>Local</option><option value="provision" {{if eq (printf "%s" .Policy) "provision"}}selected{{end}}>Provision</option><option value="blocked" {{if eq (printf "%s" .Policy) "blocked"}}selected{{end}}>Blocked</option></select><button class="op-button op-small" type="submit">Apply</button></form></td></tr>{{end}}</tbody></table></div>{{else}}<div class="empty"><h3>No machines discovered</h3><p>Boot a PXE client through discovery first.</p></div>{{end}}</section>
<section class="panel op-section"><div class="panel-head"><div><h2>Installations</h2><p>Review immutable state before arming the next boot.</p></div><a class="op-button" href="/ui/operator/installations/new">Create InstallationSpec</a></div>{{if .Installations}}<div class="table-wrap"><table><thead><tr><th>Installation</th><th>Machine</th><th>Hostname</th><th>Target disk</th><th>Assignment</th><th>Action</th></tr></thead><tbody>{{range .Installations}}<tr><td><a class="machine-link" href="/ui/installations/{{.Spec.ID}}">{{.Spec.ID}}</a></td><td><a class="machine-link" href="/ui/machines/{{.Machine.ID}}">{{.Machine.ID}}</a></td><td>{{.Spec.Profile.Hostname}}</td><td><code>{{.Spec.Storage.TargetDisk}}</code></td><td>{{if .Assignment}}<span class="badge badge-{{.Assignment.State}}">{{upper (printf "%s" .Assignment.State)}}</span>{{else}}<span class="badge badge-neutral">UNARMED</span>{{end}}</td><td>{{if not .Assignment}}{{if eq (printf "%s" .Machine.Policy) "provision"}}<form method="post" action="/ui/operator/installations/{{.Spec.ID}}/arm"><input type="hidden" name="csrf" value="{{$.Session.CSRFToken}}"><button class="op-button op-danger" type="submit">Arm next boot</button></form>{{else}}<span class="op-note">Approve machine first</span>{{end}}{{else if eq (printf "%s" .Assignment.State) "armed"}}<form method="post" action="/ui/operator/installations/{{.Spec.ID}}/cancel"><input type="hidden" name="csrf" value="{{$.Session.CSRFToken}}"><button class="op-button op-secondary" type="submit">Cancel assignment</button></form>{{else}}<span class="op-note">Immutable {{upper (printf "%s" .Assignment.State)}}</span>{{end}}</td></tr>{{end}}</tbody></table></div>{{else}}<div class="empty"><h3>No InstallationSpecs yet</h3><p>Create the first verified Debian 13 Standard installation.</p><a class="op-button" href="/ui/operator/installations/new">Open wizard</a></div>{{end}}</section></main></div></body></html>`

const installationWizardPage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>AegisPXE · New installation</title><link rel="stylesheet" href="/ui/assets/style.css"><link rel="stylesheet" href="/ui/operator/assets/operator.css"></head>
<body><div class="shell"><aside class="sidebar"><a class="brand" href="/ui/operator/"><span class="brand-mark">A</span><span><strong>AegisPXE</strong><small>Operator Console</small></span></a><nav><a href="/ui/">Read-only Studio</a><a href="/ui/installations">Installations</a><a href="/ui/operator/">Operator</a><a class="active" href="/ui/operator/installations/new">New installation</a></nav><div class="side-foot"><span class="dot"></span> Authenticated<small>{{.Session.Actor}}</small></div></aside>
<main><header><div><p class="eyebrow"><a href="/ui/operator/">OPERATOR</a> / NEW INSTALLATION</p><h1>Debian 13 Standard</h1><p class="muted">Create an immutable spec first. Arming is a separate explicit action.</p></div></header>
{{if .Error}}<div class="op-alert">{{.Error}}</div>{{end}}
<section class="panel"><div class="panel-head"><div><h2>InstallationSpec wizard</h2><p>Artifact source, hashes, driver contract and security baseline are resolved by AegisPXE and cannot be overridden here.</p></div><span class="count">Driver debian13 v1</span></div>
<form class="op-wizard" method="post" action="/ui/operator/installations"><input type="hidden" name="csrf" value="{{.Session.CSRFToken}}">
<div class="op-form-grid"><label>Machine<select name="machine_id" required>{{if .Machines}}{{range .Machines}}<option value="{{.ID}}" {{if eq $.Values.MachineID .ID}}selected{{end}}>{{.ID}} · {{upper (printf "%s" .Policy)}} · {{.Architecture}}</option>{{end}}{{else}}<option value="">No PROVISION-approved machines</option>{{end}}</select><small>Only operator-approved machines are eligible.</small></label><label>Target disk<input name="target_disk" value="{{.Values.TargetDisk}}" required><small>Whole device only. This becomes destructive only after the spec is armed and booted.</small></label><label>Hostname<input name="hostname" value="{{.Values.Hostname}}" required></label><label>Timezone<input name="timezone" value="{{.Values.Timezone}}" required></label><label>Locale<input name="locale" value="{{.Values.Locale}}" required></label><label>Keyboard<input name="keyboard" value="{{.Values.Keyboard}}" required></label><label>Admin username<input name="admin_username" value="{{.Values.AdminUsername}}" required></label><label>Admin full name<input name="admin_full_name" value="{{.Values.AdminFullName}}" required></label></div>
<label class="op-wide">SSH public key(s)<textarea name="ssh_keys" rows="6" required placeholder="ssh-ed25519 AAAA...">{{.Values.SSHKeys}}</textarea><small>One public key per line. No password is created.</small></label><label class="op-wide">Additional packages<textarea name="packages" rows="3" placeholder="curl nano qemu-guest-agent">{{.Values.Packages}}</textarea><small>Whitespace, comma or semicolon separated. OpenSSH, sudo and unattended-upgrades are enforced by the driver.</small></label>
<div class="op-fixed"><strong>Fixed security baseline</strong><span>Root login disabled</span><span>SSH passwords disabled</span><span>Automatic security updates enabled</span><span>whole-disk ext4 · unencrypted Standard slice</span></div>
<div class="op-submit"><a class="op-button op-secondary" href="/ui/operator/">Cancel</a><button class="op-button op-danger" type="submit" {{if not .Machines}}disabled{{end}}>Verify Debian artifacts & create immutable spec</button></div></form></section></main></div></body></html>`

const operatorConsoleCSS = `.operator-login-shell{max-width:620px;margin:8vh auto;padding:24px}.operator-login-panel{overflow:visible}.operator-login-body{padding:22px}.operator-login-body form,.op-wizard{display:grid;gap:18px}.operator-login-body label,.op-wizard label{display:grid;gap:7px;color:var(--muted);font-size:12px}.operator-login-body input,.op-wizard input,.op-wizard select,.op-wizard textarea,.op-inline select{width:100%;border:1px solid var(--line);border-radius:9px;background:#091519;color:var(--text);padding:10px 11px;font:inherit;outline:none}.operator-login-body input:focus,.op-wizard input:focus,.op-wizard select:focus,.op-wizard textarea:focus,.op-inline select:focus{border-color:rgba(111,255,215,.55);box-shadow:0 0 0 3px rgba(111,255,215,.07)}.operator-auth-error,.op-alert{border:1px solid rgba(255,113,133,.35);background:rgba(255,113,133,.08);color:#ffc0ca;border-radius:10px;padding:11px 13px}.op-section{margin-bottom:16px}.op-button{display:inline-flex;align-items:center;justify-content:center;gap:6px;border:1px solid rgba(111,255,215,.25);border-radius:9px;background:rgba(111,255,215,.08);color:var(--text);padding:9px 12px;text-decoration:none;font:inherit;cursor:pointer}.op-button:hover{border-color:rgba(111,255,215,.48);background:rgba(111,255,215,.12)}.op-button:disabled{opacity:.45;cursor:not-allowed}.op-secondary{border-color:var(--line);background:rgba(255,255,255,.025);color:#aac0c1}.op-danger{border-color:rgba(255,207,112,.3);background:rgba(255,207,112,.08);color:#ffe1a7}.op-small{padding:6px 9px;font-size:11px}.op-inline{display:grid;grid-template-columns:minmax(120px,1fr) auto;gap:7px;align-items:center;min-width:220px}.op-note{color:var(--muted);font-size:11px}.op-form-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px 18px}.op-wizard{padding:22px}.op-wizard small{color:#6e8c8e}.op-wide{grid-column:1/-1}.op-fixed{display:flex;flex-wrap:wrap;gap:8px;padding:14px;border:1px solid var(--line);border-radius:10px;background:rgba(91,216,255,.03)}.op-fixed strong{width:100%;font-size:12px}.op-fixed span{border:1px solid rgba(91,216,255,.14);border-radius:999px;padding:5px 8px;color:#9ab7b8;font-size:11px}.op-submit{display:flex;justify-content:flex-end;gap:9px;border-top:1px solid var(--line);padding-top:18px}.stats-four{grid-template-columns:repeat(4,minmax(130px,1fr))}@media(max-width:900px){.op-form-grid{grid-template-columns:1fr}.op-inline{grid-template-columns:1fr}.stats-four{grid-template-columns:repeat(2,minmax(120px,1fr))}}`
