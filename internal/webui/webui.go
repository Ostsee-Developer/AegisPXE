package webui

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
)

type Store interface {
	Machines(context.Context) ([]machine.Machine, error)
	Machine(context.Context, string) (machine.Machine, error)
	MachineIdentifiers(context.Context, string) ([]machine.Identifier, error)
	Events(context.Context, string, string) ([]event.Event, error)
}

type UI struct {
	store     Store
	logger    *slog.Logger
	version   string
	dashboard *template.Template
	detail    *template.Template
}

type dashboardData struct {
	Version   string
	Machines  []machine.Machine
	Pending   int
	Local     int
	Provision int
	Blocked   int
}

type machineData struct {
	Version     string
	Machine     machine.Machine
	Identifiers []machine.Identifier
	Events      []event.Event
}

func New(store Store, logger *slog.Logger, version string) *UI {
	funcs := template.FuncMap{
		"timefmt": func(value time.Time) string {
			if value.IsZero() {
				return "—"
			}
			return value.Local().Format("2006-01-02 15:04:05")
		},
		"upper": strings.ToUpper,
	}
	return &UI{
		store:     store,
		logger:    logger,
		version:   version,
		dashboard: template.Must(template.New("dashboard").Funcs(funcs).Parse(dashboardTemplate)),
		detail:    template.Must(template.New("machine").Funcs(funcs).Parse(machineTemplate)),
	}
}

func (u *UI) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /ui/assets/style.css", u.style)
	mux.HandleFunc("GET /ui/machines/{id}", u.machine)
	mux.HandleFunc("GET /ui/installations", u.installationsPage)
	mux.HandleFunc("GET /ui/installations/{id}", u.installationPage)
	mux.HandleFunc("GET /ui/", u.dashboardPage)
}

func (u *UI) dashboardPage(w http.ResponseWriter, r *http.Request) {
	machines, err := u.store.Machines(r.Context())
	if err != nil {
		u.renderError(w, r, "load dashboard", err)
		return
	}

	data := dashboardData{Version: u.version, Machines: machines}
	for _, item := range machines {
		switch item.Policy {
		case machine.PolicyPending:
			data.Pending++
		case machine.PolicyLocal:
			data.Local++
		case machine.PolicyProvision:
			data.Provision++
		case machine.PolicyBlocked:
			data.Blocked++
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := u.dashboard.Execute(w, data); err != nil {
		u.logger.ErrorContext(r.Context(), "web ui render failed", "component", "webui", "operation", "dashboard", "error", err)
	}
}

func (u *UI) machine(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	item, err := u.store.Machine(r.Context(), id)
	if err != nil {
		if fault.Code(err) == fault.MachineNotFound {
			http.NotFound(w, r)
			return
		}
		u.renderError(w, r, "load machine", err)
		return
	}
	identifiers, err := u.store.MachineIdentifiers(r.Context(), id)
	if err != nil {
		u.renderError(w, r, "load machine identifiers", err)
		return
	}
	events, err := u.store.Events(r.Context(), event.EntityMachine, id)
	if err != nil {
		u.renderError(w, r, "load machine events", err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := u.detail.Execute(w, machineData{Version: u.version, Machine: item, Identifiers: identifiers, Events: events}); err != nil {
		u.logger.ErrorContext(r.Context(), "web ui render failed", "component", "webui", "operation", "machine", "machine_id", id, "error", err)
	}
}

func (u *UI) style(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(styleCSS + installationCSS))
}

func (u *UI) renderError(w http.ResponseWriter, r *http.Request, operation string, err error) {
	u.logger.ErrorContext(r.Context(), "web ui request failed", "component", "webui", "operation", operation, "error", err)
	http.Error(w, "AegisPXE could not render this view", http.StatusInternalServerError)
}

const dashboardTemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <meta http-equiv="refresh" content="5">
  <title>AegisPXE · Machines</title>
  <link rel="stylesheet" href="/ui/assets/style.css">
</head>
<body>
<div class="shell">
  <aside class="sidebar">
    <a class="brand" href="/ui/"><span class="brand-mark">A</span><span><strong>AegisPXE</strong><small>Provisioning Control</small></span></a>
    <nav><a class="active" href="/ui/">Machines</a><a href="/ui/installations">Installations</a><span>Profiles <b>soon</b></span><span>System <b>soon</b></span></nav>
    <div class="side-foot"><span class="dot"></span> Server online<small>v{{.Version}}</small></div>
  </aside>
  <main>
    <header><div><p class="eyebrow">DISCOVERY CONTROL PLANE</p><h1>Machines</h1><p class="muted">Headless PXE clients known to AegisPXE.</p></div><div class="live"><span class="pulse"></span>Live discovery</div></header>
    <section class="stats">
      <article><span>Known machines</span><strong>{{len .Machines}}</strong></article>
      <article><span>Pending</span><strong>{{.Pending}}</strong></article>
      <article><span>Local</span><strong>{{.Local}}</strong></article>
      <article><span>Provision</span><strong>{{.Provision}}</strong></article>
      <article><span>Blocked</span><strong>{{.Blocked}}</strong></article>
    </section>
    <section class="panel">
      <div class="panel-head"><div><h2>Discovery inventory</h2><p>Machine identity stays read-only here; provisioning details now live under Installations.</p></div><span class="count">{{len .Machines}} total</span></div>
      {{if .Machines}}
      <div class="table-wrap"><table><thead><tr><th>Machine</th><th>Policy</th><th>Architecture</th><th>Firmware</th><th>First seen</th><th>Last seen</th></tr></thead><tbody>
      {{range .Machines}}<tr><td><a class="machine-link" href="/ui/machines/{{.ID}}">{{.ID}}</a></td><td><span class="badge badge-{{.Policy}}">{{upper (printf "%s" .Policy)}}</span></td><td>{{if .Architecture}}{{.Architecture}}{{else}}—{{end}}</td><td>{{if .Firmware}}{{.Firmware}}{{else}}—{{end}}</td><td>{{timefmt .FirstSeen}}</td><td>{{timefmt .LastSeen}}</td></tr>{{end}}
      </tbody></table></div>
      {{else}}
      <div class="empty"><div class="radar"><i></i></div><h3>No machines discovered yet</h3><p>Boot a test client through the AegisPXE iPXE discovery script. It will appear here as <strong>pending</strong>.</p><code>/boot/discovery.ipxe</code></div>
      {{end}}
    </section>
  </main>
</div>
</body>
</html>`

const machineTemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <meta http-equiv="refresh" content="5">
  <title>AegisPXE · {{.Machine.ID}}</title>
  <link rel="stylesheet" href="/ui/assets/style.css">
</head>
<body>
<div class="shell">
  <aside class="sidebar">
    <a class="brand" href="/ui/"><span class="brand-mark">A</span><span><strong>AegisPXE</strong><small>Provisioning Control</small></span></a>
    <nav><a class="active" href="/ui/">Machines</a><a href="/ui/installations">Installations</a><span>Profiles <b>soon</b></span><span>System <b>soon</b></span></nav>
    <div class="side-foot"><span class="dot"></span> Server online<small>v{{.Version}}</small></div>
  </aside>
  <main>
    <header><div><p class="eyebrow"><a href="/ui/">MACHINES</a> / DETAIL</p><h1>{{.Machine.ID}}</h1><p class="muted">Authoritative discovery record and audit timeline.</p></div><span class="badge badge-{{.Machine.Policy}}">{{upper (printf "%s" .Machine.Policy)}}</span></header>
    <section class="detail-grid">
      <article class="panel info"><div class="panel-head"><div><h2>Machine identity</h2><p>Observed identifiers are identity hints, not authentication.</p></div></div>
        <dl><dt>Architecture</dt><dd>{{if .Machine.Architecture}}{{.Machine.Architecture}}{{else}}—{{end}}</dd><dt>Firmware</dt><dd>{{if .Machine.Firmware}}{{.Machine.Firmware}}{{else}}—{{end}}</dd><dt>First seen</dt><dd>{{timefmt .Machine.FirstSeen}}</dd><dt>Last seen</dt><dd>{{timefmt .Machine.LastSeen}}</dd></dl>
        <h3>Identifiers</h3>{{range .Identifiers}}<div class="identifier"><span>{{.Kind}}</span><code>{{.Value}}</code></div>{{else}}<p class="muted">No identifiers stored.</p>{{end}}
      </article>
      <article class="panel"><div class="panel-head"><div><h2>Audit timeline</h2><p>Discovery and policy events recorded by the control plane.</p></div><span class="count">{{len .Events}} events</span></div>
        <div class="timeline">{{range .Events}}<div class="event"><span class="event-dot"></span><div><div class="event-top"><strong>{{.Type}}</strong><time>{{timefmt .OccurredAt}}</time></div><p>{{.Message}}</p><small>{{if .Actor}}{{.Actor}}{{end}}{{if .RequestID}} · {{.RequestID}}{{end}}{{if .ErrorCode}} · {{.ErrorCode}}{{end}}</small></div></div>{{else}}<p class="muted">No events recorded.</p>{{end}}</div>
      </article>
    </section>
  </main>
</div>
</body>
</html>`

const styleCSS = `:root{color-scheme:dark;--bg:#071013;--surface:#0d181c;--surface2:#111f24;--line:#1c343b;--text:#e7f4f2;--muted:#7f9da0;--mint:#6fffd7;--cyan:#5bd8ff;--amber:#ffcf70;--red:#ff7185;--violet:#a98cff}*{box-sizing:border-box}body{margin:0;background:radial-gradient(circle at 80% -10%,rgba(91,216,255,.08),transparent 34rem),var(--bg);color:var(--text);font:14px/1.5 Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}.shell{min-height:100vh;display:grid;grid-template-columns:245px 1fr}.sidebar{position:sticky;top:0;height:100vh;padding:28px 20px;border-right:1px solid var(--line);background:rgba(7,16,19,.88);backdrop-filter:blur(16px);display:flex;flex-direction:column}.brand{display:flex;gap:12px;align-items:center;color:var(--text);text-decoration:none}.brand-mark{width:38px;height:38px;display:grid;place-items:center;border:1px solid rgba(111,255,215,.5);border-radius:12px;background:linear-gradient(145deg,rgba(111,255,215,.16),rgba(91,216,255,.04));color:var(--mint);font-weight:900;box-shadow:0 0 28px rgba(111,255,215,.08)}.brand strong{display:block;letter-spacing:.02em}.brand small,.side-foot small{display:block;color:var(--muted);font-size:11px}.sidebar nav{display:grid;gap:7px;margin-top:38px}.sidebar nav a,.sidebar nav span{padding:11px 12px;border-radius:9px;color:#91aeb0;text-decoration:none}.sidebar nav a.active{color:var(--text);background:rgba(111,255,215,.07);border:1px solid rgba(111,255,215,.13)}.sidebar nav b{float:right;font-size:9px;text-transform:uppercase;color:#527074}.side-foot{margin-top:auto;color:#a9c2c2;font-size:12px}.side-foot .dot{display:inline-block;width:7px;height:7px;margin-right:7px;border-radius:50%;background:var(--mint);box-shadow:0 0 12px var(--mint)}main{padding:34px clamp(24px,4vw,60px)}header{display:flex;align-items:flex-start;justify-content:space-between;gap:24px;margin-bottom:28px}h1{font-size:34px;line-height:1.1;margin:3px 0 7px;letter-spacing:-.025em}h2{font-size:16px;margin:0 0 3px}h3{font-size:13px;margin:24px 0 9px}.eyebrow{font-size:10px;color:var(--mint);letter-spacing:.18em;font-weight:800;margin:0}.eyebrow a{color:inherit;text-decoration:none}.muted,.panel-head p{color:var(--muted);margin:0}.live{font-size:12px;color:#b5ccca;border:1px solid var(--line);border-radius:999px;padding:8px 12px;background:rgba(13,24,28,.7)}.pulse{display:inline-block;width:7px;height:7px;border-radius:50%;background:var(--mint);margin-right:7px;box-shadow:0 0 10px var(--mint)}.stats{display:grid;grid-template-columns:repeat(5,minmax(120px,1fr));gap:12px;margin-bottom:16px}.stats article,.panel{border:1px solid var(--line);border-radius:14px;background:linear-gradient(180deg,rgba(17,31,36,.92),rgba(11,22,26,.92));box-shadow:0 18px 50px rgba(0,0,0,.12)}.stats article{padding:16px 18px}.stats span{display:block;color:var(--muted);font-size:11px}.stats strong{font-size:24px;margin-top:5px;display:block}.panel{overflow:hidden}.panel-head{min-height:72px;padding:17px 20px;border-bottom:1px solid var(--line);display:flex;align-items:center;justify-content:space-between;gap:16px}.panel-head p{font-size:12px}.count{font-size:11px;color:#87a3a5;border:1px solid var(--line);border-radius:999px;padding:5px 9px}.table-wrap{overflow:auto}table{width:100%;border-collapse:collapse}th,td{padding:14px 20px;text-align:left;border-bottom:1px solid rgba(28,52,59,.72);white-space:nowrap}th{font-size:10px;text-transform:uppercase;letter-spacing:.08em;color:#668487;font-weight:700}td{color:#a9c1c1;font-size:12px}tbody tr:hover{background:rgba(111,255,215,.025)}tbody tr:last-child td{border-bottom:0}.machine-link{color:#dff9f2;text-decoration:none;font-family:ui-monospace,SFMono-Regular,Menlo,monospace}.machine-link:hover{color:var(--mint)}.badge{display:inline-flex;padding:5px 9px;border-radius:999px;border:1px solid;font-size:10px;letter-spacing:.06em;font-weight:800}.badge-pending{color:var(--amber);border-color:rgba(255,207,112,.26);background:rgba(255,207,112,.07)}.badge-local{color:var(--cyan);border-color:rgba(91,216,255,.24);background:rgba(91,216,255,.07)}.badge-provision{color:var(--mint);border-color:rgba(111,255,215,.26);background:rgba(111,255,215,.07)}.badge-blocked{color:var(--red);border-color:rgba(255,113,133,.26);background:rgba(255,113,133,.07)}.empty{text-align:center;padding:64px 24px;color:var(--muted)}.empty h3{color:var(--text);font-size:16px;margin:18px 0 5px}.empty p{max-width:520px;margin:0 auto 16px}.empty code,.identifier code{color:var(--mint);background:#071215;border:1px solid var(--line);padding:5px 8px;border-radius:6px}.radar{width:48px;height:48px;margin:auto;border:1px solid rgba(111,255,215,.3);border-radius:50%;display:grid;place-items:center;box-shadow:inset 0 0 20px rgba(111,255,215,.05)}.radar i{width:8px;height:8px;border-radius:50%;background:var(--mint);box-shadow:0 0 18px var(--mint)}.detail-grid{display:grid;grid-template-columns:minmax(300px,.72fr) minmax(440px,1.28fr);gap:16px}.info{padding-bottom:20px}.info dl{display:grid;grid-template-columns:120px 1fr;margin:0;padding:18px 20px;gap:9px 14px}.info dt{color:#708d90}.info dd{margin:0;color:#c3d7d6}.info h3{padding:0 20px}.identifier{margin:7px 20px;display:flex;gap:10px;align-items:center}.identifier span{width:100px;color:#708d90;font-size:11px}.identifier code{font-size:11px;overflow-wrap:anywhere}.timeline{padding:10px 20px 22px}.event{display:grid;grid-template-columns:18px 1fr;gap:10px;position:relative;padding:11px 0}.event:before{content:"";position:absolute;left:4px;top:24px;bottom:-12px;width:1px;background:var(--line)}.event:last-child:before{display:none}.event-dot{width:9px;height:9px;margin-top:5px;border-radius:50%;background:#1b3137;border:2px solid var(--mint);box-shadow:0 0 8px rgba(111,255,215,.28)}.event-top{display:flex;justify-content:space-between;gap:20px}.event-top strong{font-size:11px;color:#d7e9e6}.event-top time,.event small{color:#668184;font-size:10px}.event p{color:#9bb4b5;font-size:12px;margin:3px 0}@media(max-width:1050px){.stats{grid-template-columns:repeat(2,1fr)}.detail-grid{grid-template-columns:1fr}}@media(max-width:760px){.shell{display:block}.sidebar{position:static;height:auto;border-right:0;border-bottom:1px solid var(--line);padding:18px}.sidebar nav{display:none}.side-foot{position:absolute;right:18px;top:23px}.side-foot small{display:none}main{padding:24px 16px}.stats{grid-template-columns:repeat(2,1fr)}header{align-items:center}h1{font-size:27px}.table-wrap{margin:0}.panel-head{align-items:flex-start}.detail-grid{display:block}.detail-grid>.panel+ .panel{margin-top:16px}}`
