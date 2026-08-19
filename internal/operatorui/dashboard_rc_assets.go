package operatorui

import "net/http"

func (h *DashboardHandler) dashboardRCStyle(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write([]byte(dashboardCSS + dashboardRCTheme))
}

func (h *DashboardHandler) dashboardRCScript(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write([]byte(dashboardJS + dashboardRCJS))
}

const dashboardRCTheme = `
:root{
  color-scheme:dark;
  --bg:#10151b;
  --surface:#171e26;
  --surface-soft:#1c252e;
  --border:#293542;
  --border-strong:#3a4856;
  --text:#e7edf3;
  --muted:#8f9daa;
  --accent:#4f9f95;
  --accent-soft:#1b3432;
  --accent-strong:#75c5ba;
  --blue:#6fa7d8;
  --blue-soft:#1b2d3d;
  --warn:#d5a95b;
  --warn-soft:#352b1c;
  --danger:#df7b72;
  --danger-soft:#382321;
  --shadow:0 12px 34px rgba(0,0,0,.22);
  --radius:14px;
  --radius-sm:10px;
}
html,body{background:var(--bg);color:var(--text)}
body{font-size:14px}
a:hover{color:var(--accent-strong)}
.topbar,.sidebar,.bottom-nav{background:rgba(16,21,27,.96);border-color:var(--border)}
.sidebar{box-shadow:10px 0 34px rgba(0,0,0,.08)}
.brand-mark{background:#202b35;color:var(--accent-strong);border:1px solid var(--border-strong)}
.brand-copy strong{color:var(--text)}
.avatar{background:var(--accent-soft);color:var(--accent-strong)}
.sidebar nav a{color:#9caab6}
.sidebar nav a:hover{background:#1b242d;color:var(--text)}
.sidebar nav a.active{background:var(--accent-soft);color:var(--accent-strong);box-shadow:inset 3px 0 0 var(--accent)}
.sidebar-foot{border-color:var(--border)}
.content{width:min(100%,1500px)}
.eyebrow{color:var(--accent-strong)}
.muted,.card-subtitle,.log-status{color:var(--muted)}
.card,.stat,.form-section,.notice,.empty,.panel,.trust-card{background:var(--surface);border-color:var(--border);box-shadow:var(--shadow)}
.stat{box-shadow:none}
.button{background:var(--accent);border-color:var(--accent);color:#081311}
.button:hover{background:var(--accent-strong);border-color:var(--accent-strong)}
.button.secondary{background:#1a222b;border-color:var(--border-strong);color:var(--text)}
.button.secondary:hover{background:#222d38}
.button.ghost{color:var(--muted)}
.button.danger{background:#9d4d49;border-color:#9d4d49;color:#fff}
input,select,textarea{background:#111820;border-color:var(--border-strong);color:var(--text)}
input:focus,select:focus,textarea:focus{border-color:var(--accent);box-shadow:0 0 0 3px rgba(79,159,149,.16)}
label{color:#c4cdd5}
.meta,.card-actions,.detail-row,.event{border-color:var(--border)}
.meta dt,.detail-row span{color:var(--muted)}
.badge{background:#252f39;color:#b9c4cd}
.badge.success,.badge.active,.badge.provision,.badge.armed{background:var(--accent-soft);color:var(--accent-strong)}
.badge.pending,.badge.pending_review,.badge.enrollment_required{background:var(--warn-soft);color:var(--warn)}
.badge.blocked,.badge.cancelled{background:var(--danger-soft);color:var(--danger)}
.badge.local,.badge.consumed{background:var(--blue-soft);color:var(--blue)}
.badge.admin{background:var(--blue-soft);color:var(--blue)}
.notice.warn{border-color:#5b4826;background:var(--warn-soft);color:#e0bf7d}
.notice.danger{border-color:#633733;background:var(--danger-soft);color:#efaaa4}
.notice.success{border-color:#31524d;background:var(--accent-soft);color:var(--accent-strong)}
.empty{background:#121920;box-shadow:none}
.auth-shell{background:var(--bg)}
.auth-card{background:var(--surface);border-color:var(--border);box-shadow:0 24px 60px rgba(0,0,0,.36)}
.security-step{background:var(--surface-soft);border-color:var(--border)}
.log-view{height:min(70vh,760px);background:#0b1015;border-color:var(--border);color:#c7d1da;box-shadow:inset 0 0 0 1px rgba(255,255,255,.015)}
.log-line{border-color:#1d2730}
.log-line.warn{color:#e2bd73}.log-line.error{color:#ee9188}
.log-filter{flex:1 1 280px;min-width:180px;max-width:520px;min-height:34px;padding:6px 10px;font:12px/1.4 "SFMono-Regular",Consolas,"Liberation Mono",monospace}
.panel{overflow:hidden;border:1px solid var(--border);border-radius:var(--radius)}
.panel-head{display:flex;align-items:center;justify-content:space-between;gap:16px;min-height:70px;padding:16px 18px;border-bottom:1px solid var(--border)}
.panel-head h2,.panel-head p{margin:0}.panel-head p{margin-top:3px;color:var(--muted);font-size:12px}
.count{padding:5px 9px;border:1px solid var(--border-strong);border-radius:999px;color:var(--muted);font-size:11px;white-space:nowrap}
.table-wrap{overflow:auto}
.data-table{width:100%;border-collapse:collapse}
.data-table th,.data-table td{padding:13px 16px;text-align:left;border-bottom:1px solid var(--border);vertical-align:middle}
.data-table th{color:#798895;background:#131a21;font-size:10px;text-transform:uppercase;letter-spacing:.08em;white-space:nowrap}
.data-table td{color:#c4ced7;font-size:12px}
.data-table tbody tr:hover{background:#1a232c}
.data-table tbody tr:last-child td{border-bottom:0}
.table-actions{display:flex;align-items:center;gap:7px;min-width:250px}
.table-actions select{min-height:34px;padding:5px 8px}
.entity-link{color:#e8f1f1;font-weight:720}
.entity-link.mono{font-weight:650}
.detail-grid{display:grid;grid-template-columns:minmax(320px,.8fr) minmax(420px,1.2fr);gap:14px}
.detail-grid.equal{grid-template-columns:repeat(2,minmax(0,1fr))}
.detail-panel{padding:0}
.detail-body{padding:17px 18px}
.detail-body .meta{margin:0;padding-top:0;border-top:0}
.policy-box{margin-top:18px;padding-top:16px;border-top:1px solid var(--border)}
.timeline{display:grid;gap:0;padding:6px 18px 14px}
.event{position:relative;display:grid;grid-template-columns:12px minmax(0,1fr);gap:11px;padding:12px 0;border-bottom:1px solid var(--border)}
.event:last-child{border-bottom:0}
.event-dot{width:9px;height:9px;margin-top:5px;border:2px solid var(--accent);border-radius:50%;background:var(--surface)}
.event-top{display:flex;justify-content:space-between;gap:16px;align-items:flex-start}
.event-top strong{font-size:12px;overflow-wrap:anywhere}.event-top time{color:var(--muted);font-size:10px;white-space:nowrap}
.event p{margin:3px 0;color:#adb9c3;font-size:12px}.event small{color:#71808d;font-size:10px;overflow-wrap:anywhere}
.trust-grid{display:grid;grid-template-columns:repeat(5,minmax(130px,1fr));gap:10px;margin-bottom:14px}
.trust-card{padding:14px 15px;box-shadow:none}
.trust-card span,.trust-card small{display:block;color:var(--muted);font-size:10px}.trust-card strong{display:block;margin:5px 0;font-size:13px;color:var(--accent-strong)}
.trust-card.attention strong{color:var(--warn)}
.chips{display:flex;flex-wrap:wrap;gap:6px;margin-top:7px}.chips span{padding:4px 7px;border:1px solid var(--border);border-radius:999px;background:#111820;color:#aebbc5;font-size:10px}
.artifact-list{display:grid;gap:8px;margin-top:8px}.artifact{padding:10px;border:1px solid var(--border);border-radius:9px;background:#111820}.artifact strong,.artifact small,.artifact code{display:block}.artifact small{color:var(--muted);font-size:10px}.artifact code{margin-top:4px;color:#9fb9b5;font-size:10px;overflow-wrap:anywhere}
.overview-grid{display:grid;grid-template-columns:minmax(0,1.3fr) minmax(330px,.7fr);gap:14px}
.desktop-only{display:block}.mobile-only{display:none}

@media (min-width:960px){
  .app{grid-template-columns:248px minmax(0,1fr)}
  .content{padding:34px 38px 52px}
  .sidebar{padding:24px 14px}
  .sidebar nav a{padding:11px 12px}
  h1{font-size:31px}
}
@media (max-width:1199px){
  .trust-grid{grid-template-columns:repeat(3,minmax(0,1fr))}
  .overview-grid{grid-template-columns:1fr}
}
@media (max-width:959px){
  .desktop-only{display:none}.mobile-only{display:grid}
  .detail-grid,.detail-grid.equal{grid-template-columns:1fr}
  .trust-grid{grid-template-columns:repeat(2,minmax(0,1fr))}
  .bottom-nav{grid-template-columns:repeat(auto-fit,minmax(64px,1fr))}
}
@media (max-width:639px){
  .trust-grid{grid-template-columns:1fr 1fr}
  .event-top{display:block}.event-top time{display:block;margin-top:2px}
}
`

const dashboardRCJS = `
(() => {
  "use strict";
  const filter = document.querySelector("[data-log-filter]");
  const view = document.querySelector("[data-live-logs]");
  if (!filter || !view) return;

  const decorateLine = entry => {
    const line = document.createElement("div");
    line.className = "log-line";
    line.dataset.sequence = String(entry.sequence || "");
    try {
      const parsed = JSON.parse(entry.line);
      const level = String(parsed.level || "").toLowerCase();
      if (level === "warn" || level === "error") line.classList.add(level);
    } catch (_) {}
    line.textContent = entry.line;
    return line;
  };

  const applyFilter = () => {
    const needle = filter.value.trim().toLowerCase();
    view.querySelectorAll(".log-line").forEach(line => {
      line.hidden = needle !== "" && !line.textContent.toLowerCase().includes(needle);
    });
  };

  filter.addEventListener("input", applyFilter);
  new MutationObserver(applyFilter).observe(view, {childList: true});

  const anchor = Number(view.dataset.after || "0");
  fetch("/ui/api/logs/tail?before=" + encodeURIComponent(anchor), {credentials:"same-origin", headers:{"Accept":"application/json"}})
    .then(response => response.ok ? response.json() : Promise.reject(new Error("tail unavailable")))
    .then(payload => {
      const fragment = document.createDocumentFragment();
      (payload.entries || []).forEach(entry => fragment.appendChild(decorateLine(entry)));
      view.prepend(fragment);
      applyFilter();
      view.scrollTop = view.scrollHeight;
    })
    .catch(() => {});
})();
`
