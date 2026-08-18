package webui

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/assignment"
	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/installation"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
	"github.com/Ostsee-Developer/AegisPXE/internal/trust"
)

type installationReader interface {
	InstallationSpecs(context.Context) ([]installation.Spec, error)
	InstallationSpec(context.Context, string) (installation.Spec, error)
	AssignmentForInstallation(context.Context, string) (assignment.Assignment, error)
}

type installationRow struct {
	Spec       installation.Spec
	Machine    machine.Machine
	Assignment *assignment.Assignment
	Gate       trust.Gate
}

type installationsData struct {
	Version          string
	Rows             []installationRow
	Armed            int
	PublicBootReady  int
	SecretReady      int
}

type installationData struct {
	Version     string
	Spec        installation.Spec
	Machine     machine.Machine
	Assignment  *assignment.Assignment
	Gate        trust.Gate
	Events      []event.Event
}

var installationTemplateFuncs = template.FuncMap{
	"timefmt": func(value time.Time) string {
		if value.IsZero() {
			return "—"
		}
		return value.Local().Format("2006-01-02 15:04:05")
	},
	"upper": strings.ToUpper,
	"bytesize": func(value int64) string {
		switch {
		case value >= 1024*1024:
			return fmt.Sprintf("%.1f MiB", float64(value)/(1024*1024))
		case value >= 1024:
			return fmt.Sprintf("%.1f KiB", float64(value)/1024)
		default:
			return fmt.Sprintf("%d B", value)
		}
	},
}

var installationsTemplate = template.Must(template.New("installations").Funcs(installationTemplateFuncs).Parse(installationsPageTemplate))
var installationDetailTemplate = template.Must(template.New("installation").Funcs(installationTemplateFuncs).Parse(installationDetailPageTemplate))

func (u *UI) installationsPage(w http.ResponseWriter, r *http.Request) {
	reader, ok := u.store.(installationReader)
	if !ok {
		u.renderError(w, r, "load installations", fmt.Errorf("store does not implement installation reader"))
		return
	}
	specs, err := reader.InstallationSpecs(r.Context())
	if err != nil {
		u.renderError(w, r, "load installations", err)
		return
	}
	data := installationsData{Version: u.version, Rows: make([]installationRow, 0, len(specs))}
	for _, spec := range specs {
		item, err := u.store.Machine(r.Context(), spec.MachineID)
		if err != nil {
			u.renderError(w, r, "load installation machine", err)
			return
		}
		row := installationRow{Spec: spec, Machine: item}
		armed := false
		storedAssignment, err := reader.AssignmentForInstallation(r.Context(), spec.ID)
		if err == nil {
			row.Assignment = &storedAssignment
			armed = storedAssignment.State == assignment.StateArmed
			if armed {
				data.Armed++
			}
		} else if fault.Code(err) != fault.InstallationAssignmentNotFound {
			u.renderError(w, r, "load installation assignment", err)
			return
		}
		row.Gate = trust.Evaluate(item.Policy, armed, false)
		if row.Gate.PublicBootAllowed {
			data.PublicBootReady++
		}
		if row.Gate.SecretReleaseAllowed {
			data.SecretReady++
		}
		data.Rows = append(data.Rows, row)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := installationsTemplate.Execute(w, data); err != nil {
		u.logger.ErrorContext(r.Context(), "web ui render failed", "component", "webui", "operation", "installations", "error", err)
	}
}

func (u *UI) installationPage(w http.ResponseWriter, r *http.Request) {
	reader, ok := u.store.(installationReader)
	if !ok {
		u.renderError(w, r, "load installation", fmt.Errorf("store does not implement installation reader"))
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	spec, err := reader.InstallationSpec(r.Context(), id)
	if err != nil {
		if fault.Code(err) == fault.InstallationNotFound {
			http.NotFound(w, r)
			return
		}
		u.renderError(w, r, "load installation", err)
		return
	}
	item, err := u.store.Machine(r.Context(), spec.MachineID)
	if err != nil {
		u.renderError(w, r, "load installation machine", err)
		return
	}
	var storedAssignment *assignment.Assignment
	assignmentItem, err := reader.AssignmentForInstallation(r.Context(), spec.ID)
	if err == nil {
		storedAssignment = &assignmentItem
	} else if fault.Code(err) != fault.InstallationAssignmentNotFound {
		u.renderError(w, r, "load installation assignment", err)
		return
	}
	armed := storedAssignment != nil && storedAssignment.State == assignment.StateArmed
	gate := trust.Evaluate(item.Policy, armed, false)
	events, err := u.store.Events(r.Context(), event.EntityInstallation, spec.ID)
	if err != nil {
		u.renderError(w, r, "load installation events", err)
		return
	}

	data := installationData{
		Version:    u.version,
		Spec:       spec,
		Machine:    item,
		Assignment: storedAssignment,
		Gate:       gate,
		Events:     events,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := installationDetailTemplate.Execute(w, data); err != nil {
		u.logger.ErrorContext(r.Context(), "web ui render failed", "component", "webui", "operation", "installation", "installation_id", spec.ID, "machine_id", spec.MachineID, "error", err)
	}
}

const installationsPageTemplate = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta http-equiv="refresh" content="5"><title>AegisPXE · Installations</title><link rel="stylesheet" href="/ui/assets/style.css"></head>
<body><div class="shell"><aside class="sidebar"><a class="brand" href="/ui/"><span class="brand-mark">A</span><span><strong>AegisPXE</strong><small>Provisioning Control</small></span></a><nav><a href="/ui/">Machines</a><a class="active" href="/ui/installations">Installations</a><span>Profiles <b>soon</b></span><span>System <b>soon</b></span></nav><div class="side-foot"><span class="dot"></span> Server online<small>v{{.Version}}</small></div></aside>
<main><header><div><p class="eyebrow">PROVISIONING CONTROL PLANE</p><h1>Installations</h1><p class="muted">Immutable desired state, assignment and trust gates.</p></div><div class="live"><span class="pulse"></span>Read-only trust view</div></header>
<section class="stats stats-four"><article><span>Installation specs</span><strong>{{len .Rows}}</strong></article><article><span>Armed</span><strong>{{.Armed}}</strong></article><article><span>Public boot ready</span><strong>{{.PublicBootReady}}</strong></article><article><span>Secret ready</span><strong>{{.SecretReady}}</strong></article></section>
<section class="panel"><div class="panel-head"><div><h2>Immutable installation inventory</h2><p>Studio shows current trust facts. Administrative mutations remain disabled until operator authentication exists.</p></div><span class="count">{{len .Rows}} total</span></div>
{{if .Rows}}<div class="table-wrap"><table><thead><tr><th>Installation</th><th>Machine</th><th>OS</th><th>Profile</th><th>Target disk</th><th>Assignment</th><th>Trust gate</th><th>Created</th></tr></thead><tbody>
{{range .Rows}}<tr><td><a class="machine-link" href="/ui/installations/{{.Spec.ID}}">{{.Spec.ID}}</a></td><td><a class="machine-link" href="/ui/machines/{{.Machine.ID}}">{{.Machine.ID}}</a></td><td>{{.Spec.DriverID}} · {{.Spec.OSRelease}}</td><td>{{.Spec.ProfileID}} · {{.Spec.ProfileRevision}}</td><td><code>{{.Spec.Storage.TargetDisk}}</code></td><td>{{if .Assignment}}<span class="badge badge-{{.Assignment.State}}">{{upper (printf "%s" .Assignment.State)}}</span>{{else}}<span class="badge badge-neutral">UNARMED</span>{{end}}</td><td>{{if .Gate.SecretReleaseAllowed}}<span class="trust-ok">SECRET READY</span>{{else if .Gate.PublicBootAllowed}}<span class="trust-warn">BOOT ONLY</span>{{else}}<span class="trust-block">BLOCKED</span>{{end}}</td><td>{{timefmt .Spec.CreatedAt}}</td></tr>{{end}}</tbody></table></div>
{{else}}<div class="empty"><div class="radar"><i></i></div><h3>No InstallationSpecs yet</h3><p>The Debian driver is ready to render immutable specs, but creation and arming remain behind the future authenticated operator boundary.</p></div>{{end}}</section></main></div></body></html>`

const installationDetailPageTemplate = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta http-equiv="refresh" content="5"><title>AegisPXE · {{.Spec.ID}}</title><link rel="stylesheet" href="/ui/assets/style.css"></head>
<body><div class="shell"><aside class="sidebar"><a class="brand" href="/ui/"><span class="brand-mark">A</span><span><strong>AegisPXE</strong><small>Provisioning Control</small></span></a><nav><a href="/ui/">Machines</a><a class="active" href="/ui/installations">Installations</a><span>Profiles <b>soon</b></span><span>System <b>soon</b></span></nav><div class="side-foot"><span class="dot"></span> Server online<small>v{{.Version}}</small></div></aside>
<main><header><div><p class="eyebrow"><a href="/ui/installations">INSTALLATIONS</a> / IMMUTABLE SPEC</p><h1>{{.Spec.ID}}</h1><p class="muted">{{.Spec.DriverID}} v{{.Spec.DriverVersion}} · Debian {{.Spec.OSRelease}} · {{.Spec.Architecture}}</p></div>{{if .Assignment}}<span class="badge badge-{{.Assignment.State}}">{{upper (printf "%s" .Assignment.State)}}</span>{{else}}<span class="badge badge-neutral">UNARMED</span>{{end}}</header>
<section class="trust-grid"><article class="trust-card"><span>Operator approval</span><strong>{{if .Gate.OperatorApproved}}APPROVED{{else}}REQUIRED{{end}}</strong><small>Machine policy: {{upper (printf "%s" .Machine.Policy)}}</small></article><article class="trust-card"><span>Assignment</span><strong>{{if .Gate.AssignmentArmed}}ARMED{{else}}NOT ARMED{{end}}</strong><small>{{if .Assignment}}{{.Assignment.ID}}{{else}}No active assignment{{end}}</small></article><article class="trust-card"><span>Public boot material</span><strong>{{if .Gate.PublicBootAllowed}}ALLOWED{{else}}BLOCKED{{end}}</strong><small>Contains no reusable secret</small></article><article class="trust-card trust-card-critical"><span>Cryptographic boot trust</span><strong>{{if .Gate.CryptographicVerified}}VERIFIED{{else}}REQUIRED{{end}}</strong><small>{{.Gate.Reason}}</small></article><article class="trust-card trust-card-critical"><span>Secret release</span><strong>{{if .Gate.SecretReleaseAllowed}}ALLOWED{{else}}BLOCKED{{end}}</strong><small>Lifecycle credentials stay behind cryptographic proof</small></article></section>
<section class="detail-grid installation-grid"><article class="panel info"><div class="panel-head"><div><h2>InstallationSpec</h2><p>Immutable desired-state snapshot.</p></div></div><dl><dt>Machine</dt><dd><a class="machine-link" href="/ui/machines/{{.Machine.ID}}">{{.Machine.ID}}</a></dd><dt>Driver</dt><dd>{{.Spec.DriverID}} v{{.Spec.DriverVersion}}</dd><dt>OS</dt><dd>Debian {{.Spec.OSRelease}} · {{.Spec.Architecture}}</dd><dt>Profile</dt><dd>{{.Spec.ProfileID}} · {{.Spec.ProfileRevision}}</dd><dt>Created</dt><dd>{{timefmt .Spec.CreatedAt}}</dd><dt>Created by</dt><dd>{{.Spec.CreatedBy}}</dd></dl><h3>Storage</h3><dl><dt>Mode</dt><dd>{{.Spec.Storage.Mode}}</dd><dt>Filesystem</dt><dd>{{.Spec.Storage.Filesystem}}</dd><dt>Target disk</dt><dd><code>{{.Spec.Storage.TargetDisk}}</code></dd><dt>Encrypted</dt><dd>{{.Spec.Storage.Encrypted}}</dd><dt>TPM2</dt><dd>{{.Spec.Storage.TPM2}}</dd></dl><h3>Security</h3><dl><dt>Root login</dt><dd>{{.Spec.Security.RootLogin}}</dd><dt>SSH passwords</dt><dd>{{.Spec.Security.SSHPasswordAuthentication}}</dd><dt>Security updates</dt><dd>{{.Spec.Security.AutomaticSecurityUpdates}}</dd></dl></article>
<article class="panel info"><div class="panel-head"><div><h2>Resolved profile</h2><p>Values copied into the spec, not live profile state.</p></div></div><dl><dt>Hostname</dt><dd>{{.Spec.Profile.Hostname}}</dd><dt>Locale</dt><dd>{{.Spec.Profile.Locale}}</dd><dt>Keyboard</dt><dd>{{.Spec.Profile.Keyboard}}</dd><dt>Timezone</dt><dd>{{.Spec.Profile.Timezone}}</dd><dt>Admin</dt><dd>{{.Spec.Profile.Admin.Username}} · {{.Spec.Profile.Admin.FullName}}</dd><dt>SSH keys</dt><dd>{{len .Spec.Profile.Admin.AuthorizedSSHKeys}} pinned public key(s)</dd><dt>Passwordless sudo</dt><dd>{{.Spec.Profile.Admin.PasswordlessSudo}}</dd></dl><h3>Packages</h3><div class="chips">{{range .Spec.Profile.Packages}}<span>{{.}}</span>{{else}}<span>standard only</span>{{end}}</div><h3>Verified artifacts</h3>{{range .Spec.Artifacts}}<div class="artifact"><div><strong>{{.Name}}</strong><small>{{.ID}} · {{bytesize .Size}} · {{.Version}}</small></div><code>{{.Digest}}</code><small>{{.Provenance}}</small></div>{{end}}</article></section>
<section class="panel timeline-panel"><div class="panel-head"><div><h2>Installation audit timeline</h2><p>Only accepted server mutations appear here. Boot downloads do not invent lifecycle progress.</p></div><span class="count">{{len .Events}} events</span></div><div class="timeline">{{range .Events}}<div class="event"><span class="event-dot"></span><div><div class="event-top"><strong>{{.Type}}</strong><time>{{timefmt .OccurredAt}}</time></div><p>{{.Message}}</p><small>{{if .Actor}}{{.Actor}}{{end}}{{if .RequestID}} · {{.RequestID}}{{end}}{{if .ErrorCode}} · {{.ErrorCode}}{{end}}</small></div></div>{{else}}<p class="muted">No installation events recorded.</p>{{end}}</div></section></main></div></body></html>`

const installationCSS = `.stats-four{grid-template-columns:repeat(4,minmax(140px,1fr))}.badge-armed{color:var(--mint);border-color:rgba(111,255,215,.26);background:rgba(111,255,215,.07)}.badge-consumed{color:var(--cyan);border-color:rgba(91,216,255,.24);background:rgba(91,216,255,.07)}.badge-cancelled{color:var(--red);border-color:rgba(255,113,133,.26);background:rgba(255,113,133,.07)}.badge-neutral{color:#8ca6a8;border-color:var(--line);background:rgba(127,157,160,.05)}.trust-ok{color:var(--mint);font-weight:800;font-size:10px}.trust-warn{color:var(--amber);font-weight:800;font-size:10px}.trust-block{color:var(--red);font-weight:800;font-size:10px}.trust-grid{display:grid;grid-template-columns:repeat(5,minmax(150px,1fr));gap:12px;margin-bottom:16px}.trust-card{padding:16px 18px;border:1px solid var(--line);border-radius:14px;background:linear-gradient(180deg,rgba(17,31,36,.92),rgba(11,22,26,.92))}.trust-card span,.trust-card small{display:block;color:var(--muted);font-size:10px}.trust-card strong{display:block;margin:5px 0;color:var(--mint);font-size:14px}.trust-card-critical strong{color:var(--amber)}.installation-grid{grid-template-columns:repeat(2,minmax(340px,1fr));margin-bottom:16px}.chips{display:flex;gap:6px;flex-wrap:wrap;padding:0 20px}.chips span{padding:4px 8px;border:1px solid var(--line);border-radius:999px;color:#9db6b7;font-size:10px}.artifact{margin:8px 20px;padding:10px 12px;border:1px solid var(--line);border-radius:9px;background:#091417}.artifact strong,.artifact small,.artifact code{display:block}.artifact small{color:var(--muted);font-size:10px;margin-top:2px}.artifact code{color:var(--mint);font-size:9px;overflow-wrap:anywhere;margin-top:6px}.timeline-panel{margin-top:0}@media(max-width:1200px){.trust-grid{grid-template-columns:repeat(2,1fr)}}@media(max-width:760px){.stats-four,.trust-grid{grid-template-columns:repeat(2,1fr)}.installation-grid{display:block}.installation-grid>.panel+.panel{margin-top:16px}}`
