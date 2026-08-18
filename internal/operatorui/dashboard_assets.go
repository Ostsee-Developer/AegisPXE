package operatorui

import (
	"net/http"
)

func (h *DashboardHandler) dashboardStyle(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(dashboardCSS))
}

func (h *DashboardHandler) dashboardScript(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(dashboardJS))
}

const dashboardCSS = `:root{
  color-scheme:light;
  --bg:#f5f7fa;
  --surface:#ffffff;
  --surface-soft:#f9fbfc;
  --border:#dfe6eb;
  --border-strong:#ccd7de;
  --text:#17212b;
  --muted:#647481;
  --accent:#0f766e;
  --accent-soft:#e7f6f3;
  --accent-strong:#0b5f59;
  --blue:#2563eb;
  --blue-soft:#eef4ff;
  --warn:#a16207;
  --warn-soft:#fff8e6;
  --danger:#b42318;
  --danger-soft:#fff0ee;
  --shadow:0 1px 2px rgba(15,23,42,.04),0 8px 28px rgba(15,23,42,.05);
  --radius:16px;
  --radius-sm:11px;
  font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;
}
*{box-sizing:border-box}
html{background:var(--bg)}
body{margin:0;background:var(--bg);color:var(--text);font-size:15px;line-height:1.5}
a{color:inherit;text-decoration:none}
button,input,select,textarea{font:inherit}
button{cursor:pointer}
code,.mono{font-family:"SFMono-Regular",Consolas,"Liberation Mono",monospace;font-size:.92em;overflow-wrap:anywhere}
.app{min-height:100vh;padding-bottom:76px}
.topbar{position:sticky;top:0;z-index:20;display:flex;align-items:center;justify-content:space-between;gap:12px;padding:14px 16px;background:rgba(255,255,255,.94);backdrop-filter:blur(14px);border-bottom:1px solid var(--border)}
.brand{display:flex;align-items:center;gap:10px;min-width:0}
.brand-mark{display:grid;place-items:center;width:34px;height:34px;border-radius:10px;background:var(--text);color:white;font-weight:800;letter-spacing:-.04em}
.brand-copy{min-width:0}
.brand-copy strong{display:block;font-size:15px;line-height:1.1}
.brand-copy span{display:block;margin-top:3px;color:var(--muted);font-size:11px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;max-width:48vw}
.avatar{display:grid;place-items:center;width:34px;height:34px;border-radius:50%;background:var(--accent-soft);color:var(--accent-strong);font-weight:800;font-size:12px}
.sidebar{display:none}
.content{width:min(100%,1280px);margin:0 auto;padding:20px 16px 28px}
.page-head{display:flex;flex-direction:column;gap:14px;margin-bottom:20px}
.eyebrow{margin:0 0 5px;color:var(--accent);font-size:11px;font-weight:800;letter-spacing:.12em;text-transform:uppercase}
h1,h2,h3,p{margin-top:0}
h1{margin-bottom:5px;font-size:27px;line-height:1.15;letter-spacing:-.035em}
h2{margin-bottom:4px;font-size:18px;letter-spacing:-.02em}
h3{margin-bottom:4px;font-size:15px}
.muted{color:var(--muted)}
.page-head .muted{margin-bottom:0}
.actions{display:flex;flex-wrap:wrap;gap:9px}
.button{display:inline-flex;align-items:center;justify-content:center;gap:8px;min-height:42px;padding:9px 14px;border:1px solid var(--accent);border-radius:10px;background:var(--accent);color:#fff;font-weight:750;box-shadow:none}
.button:hover{background:var(--accent-strong)}
.button.secondary{background:var(--surface);border-color:var(--border-strong);color:var(--text)}
.button.secondary:hover{background:var(--surface-soft)}
.button.danger{background:var(--danger);border-color:var(--danger)}
.button.ghost{background:transparent;border-color:transparent;color:var(--muted)}
.button.small{min-height:34px;padding:6px 10px;font-size:13px}
.button:disabled{opacity:.55;cursor:not-allowed}
.stats{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px;margin-bottom:18px}
.stat{padding:15px;border:1px solid var(--border);border-radius:var(--radius-sm);background:var(--surface);box-shadow:0 1px 1px rgba(15,23,42,.025)}
.stat span{display:block;color:var(--muted);font-size:12px;font-weight:650}
.stat strong{display:block;margin-top:4px;font-size:24px;letter-spacing:-.04em}
.grid{display:grid;grid-template-columns:1fr;gap:12px}
.card{padding:16px;border:1px solid var(--border);border-radius:var(--radius);background:var(--surface);box-shadow:var(--shadow)}
.card-head{display:flex;align-items:flex-start;justify-content:space-between;gap:12px;margin-bottom:13px}
.card-head p{margin-bottom:0}
.card-title{font-weight:800;letter-spacing:-.01em;overflow-wrap:anywhere}
.card-subtitle{margin-top:2px;color:var(--muted);font-size:12px}
.meta{display:grid;grid-template-columns:1fr 1fr;gap:12px;padding-top:12px;border-top:1px solid var(--border)}
.meta div{min-width:0}
.meta dt{color:var(--muted);font-size:11px;font-weight:700;text-transform:uppercase;letter-spacing:.06em}
.meta dd{margin:3px 0 0;font-weight:650;overflow-wrap:anywhere}
.badge{display:inline-flex;align-items:center;gap:6px;max-width:100%;padding:5px 8px;border-radius:999px;background:#eef2f5;color:#44525e;font-size:11px;font-weight:800;letter-spacing:.04em;text-transform:uppercase;white-space:nowrap}
.badge.success,.badge.active,.badge.provision,.badge.armed{background:var(--accent-soft);color:var(--accent-strong)}
.badge.pending,.badge.pending_review,.badge.enrollment_required{background:var(--warn-soft);color:var(--warn)}
.badge.blocked,.badge.cancelled{background:var(--danger-soft);color:var(--danger)}
.badge.admin{background:var(--blue-soft);color:var(--blue)}
.card-actions{display:flex;flex-wrap:wrap;gap:8px;margin-top:14px;padding-top:14px;border-top:1px solid var(--border)}
.inline-form{display:flex;flex-wrap:wrap;gap:8px;align-items:center;width:100%}
.inline-form select{flex:1 1 150px}
input,select,textarea{width:100%;min-height:43px;padding:9px 11px;border:1px solid var(--border-strong);border-radius:10px;background:#fff;color:var(--text);outline:none;transition:border-color .15s,box-shadow .15s}
textarea{min-height:120px;resize:vertical}
input:focus,select:focus,textarea:focus{border-color:var(--accent);box-shadow:0 0 0 3px rgba(15,118,110,.10)}
label{display:grid;gap:6px;color:#34434f;font-size:13px;font-weight:700}
label small{color:var(--muted);font-weight:500;line-height:1.4}
.form-grid{display:grid;grid-template-columns:1fr;gap:14px}
.form-section{display:grid;gap:14px;padding:16px;border:1px solid var(--border);border-radius:var(--radius);background:var(--surface)}
.notice{padding:13px 14px;border:1px solid var(--border);border-radius:12px;background:var(--surface-soft);color:#3d4b56}
.notice.warn{border-color:#f0d49a;background:var(--warn-soft);color:#765008}
.notice.danger{border-color:#f0b8b1;background:var(--danger-soft);color:#8f2118}
.notice.success{border-color:#b8dfd8;background:var(--accent-soft);color:var(--accent-strong)}
.empty{padding:28px 18px;text-align:center;border:1px dashed var(--border-strong);border-radius:var(--radius);background:rgba(255,255,255,.55)}
.empty p{margin-bottom:0;color:var(--muted)}
.section{margin-top:20px}
.section-head{display:flex;align-items:flex-end;justify-content:space-between;gap:12px;margin-bottom:10px}
.section-head p{margin:0;color:var(--muted);font-size:13px}
.timeline{display:grid;gap:9px}
.event{display:grid;grid-template-columns:8px 1fr;gap:10px;padding:10px 0;border-bottom:1px solid var(--border)}
.event:last-child{border-bottom:0}
.event-dot{width:8px;height:8px;margin-top:7px;border-radius:50%;background:var(--accent)}
.event p{margin:0}
.event small{color:var(--muted)}
.bottom-nav{position:fixed;left:0;right:0;bottom:0;z-index:30;display:grid;grid-template-columns:repeat(5,1fr);padding:7px 7px calc(7px + env(safe-area-inset-bottom));border-top:1px solid var(--border);background:rgba(255,255,255,.97);backdrop-filter:blur(14px)}
.bottom-nav a{display:grid;place-items:center;gap:2px;min-height:48px;border-radius:10px;color:var(--muted);font-size:10px;font-weight:750}
.bottom-nav a.active{background:var(--accent-soft);color:var(--accent-strong)}
.nav-icon{font-size:16px;line-height:1}
.auth-shell{display:grid;min-height:100vh;place-items:center;padding:20px;background:linear-gradient(180deg,#f8fbfb 0%,var(--bg) 55%)}
.auth-card{width:min(100%,430px);padding:22px;border:1px solid var(--border);border-radius:20px;background:var(--surface);box-shadow:0 18px 50px rgba(15,23,42,.08)}
.auth-card .brand{margin-bottom:22px}
.auth-card h1{font-size:25px}
.auth-card form{display:grid;gap:13px;margin-top:18px}
.auth-actions{display:grid;gap:9px;margin-top:16px}
.security-chain{display:grid;grid-template-columns:1fr;gap:8px;margin:18px 0}
.security-step{display:flex;align-items:center;gap:9px;padding:10px 11px;border:1px solid var(--border);border-radius:11px;background:var(--surface-soft);font-size:13px}
.security-step strong{margin-left:auto;color:var(--accent)}
.log-toolbar{display:flex;flex-wrap:wrap;align-items:center;gap:8px;margin-bottom:10px}
.log-status{margin-left:auto;color:var(--muted);font-size:12px}
.log-view{height:min(62vh,640px);overflow:auto;padding:12px;border:1px solid #d8e0e6;border-radius:12px;background:#fbfcfd;color:#26323c;font:12px/1.55 "SFMono-Regular",Consolas,"Liberation Mono",monospace;white-space:pre-wrap;overflow-wrap:anywhere}
.log-line{padding:5px 0;border-bottom:1px solid #edf1f4}
.log-line:last-child{border-bottom:0}
.log-line.warn{color:#805a0b}.log-line.error{color:#9f241a}
.detail-list{display:grid;gap:10px}
.detail-row{display:grid;gap:3px;padding-bottom:10px;border-bottom:1px solid var(--border)}
.detail-row:last-child{border-bottom:0;padding-bottom:0}
.detail-row span{color:var(--muted);font-size:11px;font-weight:750;text-transform:uppercase;letter-spacing:.05em}
.detail-row strong{overflow-wrap:anywhere}

@media (min-width:640px){
  .content{padding:26px 24px 40px}
  .page-head{flex-direction:row;align-items:flex-end;justify-content:space-between}
  .stats{grid-template-columns:repeat(4,minmax(0,1fr))}
  .grid.two{grid-template-columns:repeat(2,minmax(0,1fr))}
  .form-grid{grid-template-columns:repeat(2,minmax(0,1fr))}
  .form-grid .wide{grid-column:1/-1}
}
@media (min-width:960px){
  .app{display:grid;grid-template-columns:232px minmax(0,1fr);padding-bottom:0}
  .topbar{display:none}
  .sidebar{position:sticky;top:0;display:flex;flex-direction:column;height:100vh;padding:22px 14px;border-right:1px solid var(--border);background:#fff}
  .sidebar .brand{padding:0 8px 22px}
  .sidebar nav{display:grid;gap:4px}
  .sidebar nav a{display:flex;align-items:center;gap:10px;padding:10px 11px;border-radius:10px;color:#52616d;font-size:13px;font-weight:700}
  .sidebar nav a.active{background:var(--accent-soft);color:var(--accent-strong)}
  .sidebar-foot{margin-top:auto;padding:14px 9px 4px;border-top:1px solid var(--border)}
  .sidebar-foot strong,.sidebar-foot small{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .sidebar-foot small{margin-top:3px;color:var(--muted)}
  .bottom-nav{display:none}
  .content{width:min(100%,1320px);padding:34px 34px 48px}
  .grid.three{grid-template-columns:repeat(3,minmax(0,1fr))}
}
@media (prefers-reduced-motion:reduce){*{scroll-behavior:auto!important;transition:none!important}}
`

const dashboardJS = `(() => {
  "use strict";

  const b64ToBytes = value => {
    const normalized = value.replace(/-/g, "+").replace(/_/g, "/");
    const padded = normalized + "=".repeat((4 - normalized.length % 4) % 4);
    const raw = atob(padded);
    return Uint8Array.from(raw, c => c.charCodeAt(0));
  };
  const bytesToB64 = value => {
    const bytes = new Uint8Array(value);
    let raw = "";
    bytes.forEach(b => { raw += String.fromCharCode(b); });
    return btoa(raw).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
  };
  const prepareCreation = options => {
    const copy = structuredClone(options);
    copy.challenge = b64ToBytes(copy.challenge);
    copy.user.id = b64ToBytes(copy.user.id);
    (copy.excludeCredentials || []).forEach(item => { item.id = b64ToBytes(item.id); });
    return copy;
  };
  const prepareAssertion = options => {
    const copy = structuredClone(options);
    copy.challenge = b64ToBytes(copy.challenge);
    (copy.allowCredentials || []).forEach(item => { item.id = b64ToBytes(item.id); });
    return copy;
  };
  const credentialJSON = credential => {
    const response = credential.response;
    const data = {
      id: credential.id,
      rawId: bytesToB64(credential.rawId),
      type: credential.type,
      response: {}
    };
    if (response.clientDataJSON) data.response.clientDataJSON = bytesToB64(response.clientDataJSON);
    if (response.authenticatorData) data.response.authenticatorData = bytesToB64(response.authenticatorData);
    if (response.signature) data.response.signature = bytesToB64(response.signature);
    if (response.userHandle) data.response.userHandle = bytesToB64(response.userHandle);
    if (response.attestationObject) data.response.attestationObject = bytesToB64(response.attestationObject);
    if (typeof response.getTransports === "function") data.response.transports = response.getTransports();
    if (credential.authenticatorAttachment) data.authenticatorAttachment = credential.authenticatorAttachment;
    if (typeof credential.getClientExtensionResults === "function") data.clientExtensionResults = credential.getClientExtensionResults();
    return data;
  };
  const post = async (url, body, headers = {}) => {
    const response = await fetch(url, {method:"POST", body, headers:{"Accept":"application/json", ...headers}, credentials:"same-origin"});
    const text = await response.text();
    let data = {};
    if (text) { try { data = JSON.parse(text); } catch (_) { data = {message:text}; } }
    if (!response.ok) throw new Error(data.message || "Authentication failed");
    return data;
  };
  const setMessage = (node, message, kind = "danger") => {
    if (!node) return;
    node.className = "notice " + kind;
    node.textContent = message;
    node.hidden = false;
  };

  const loginButton = document.querySelector("[data-passkey-login]");
  if (loginButton) loginButton.addEventListener("click", async () => {
    const message = document.querySelector("[data-auth-message]");
    loginButton.disabled = true;
    try {
      if (!window.PublicKeyCredential) throw new Error("This browser does not support passkeys.");
      const start = await post("/ui/api/passkey/login/start", "");
      const credential = await navigator.credentials.get({publicKey:prepareAssertion(start.options.publicKey)});
      await post("/ui/api/passkey/login/finish", JSON.stringify(credentialJSON(credential)), {"Content-Type":"application/json", "X-AegisPXE-Ceremony":start.flow});
      location.assign("/ui/");
    } catch (error) {
      setMessage(message, error.message || "Passkey login failed");
      loginButton.disabled = false;
    }
  });

  const enrollButton = document.querySelector("[data-passkey-enroll]");
  if (enrollButton) enrollButton.addEventListener("click", async () => {
    const message = document.querySelector("[data-auth-message]");
    enrollButton.disabled = true;
    try {
      if (!window.PublicKeyCredential) throw new Error("This browser does not support passkeys.");
      const start = await post("/ui/api/passkey/enroll/start", "");
      const credential = await navigator.credentials.create({publicKey:prepareCreation(start.options.publicKey)});
      await post("/ui/api/passkey/enroll/finish", JSON.stringify(credentialJSON(credential)), {"Content-Type":"application/json", "X-AegisPXE-Ceremony":start.flow});
      location.assign("/ui/");
    } catch (error) {
      setMessage(message, error.message || "Passkey enrollment failed");
      enrollButton.disabled = false;
    }
  });

  const recoveryButton = document.querySelector("[data-recovery-passkey]");
  if (recoveryButton) recoveryButton.addEventListener("click", async () => {
    const subject = (document.querySelector("[name=recovery_subject]") || {}).value || "";
    const message = document.querySelector("[data-auth-message]");
    recoveryButton.disabled = true;
    try {
      if (!subject.trim()) throw new Error("Enter your AegisPXE username first.");
      if (!window.PublicKeyCredential) throw new Error("This browser does not support passkeys.");
      const body = new URLSearchParams({subject});
      const start = await post("/ui/api/recovery/start", body, {"Content-Type":"application/x-www-form-urlencoded"});
      const credential = await navigator.credentials.get({publicKey:prepareAssertion(start.options.publicKey)});
      const finished = await post("/ui/api/recovery/passkey", JSON.stringify(credentialJSON(credential)), {"Content-Type":"application/json", "X-AegisPXE-Ceremony":start.flow, "X-AegisPXE-Recovery-Subject":subject});
      const finalForm = document.querySelector("[data-recovery-key-form]");
      finalForm.querySelector("[name=subject]").value = subject;
      finalForm.querySelector("[name=ticket]").value = finished.ticket;
      finalForm.hidden = false;
      recoveryButton.hidden = true;
      document.querySelector("[name=recovery_subject]").readOnly = true;
      setMessage(message, "Passkey verified. Enter the local recovery key to finish.", "success");
    } catch (error) {
      setMessage(message, "Recovery authentication failed.");
      recoveryButton.disabled = false;
    }
  });

  const logView = document.querySelector("[data-live-logs]");
  if (logView) {
    let after = Number(logView.dataset.after || "0");
    let paused = false;
    const status = document.querySelector("[data-log-status]");
    const pause = document.querySelector("[data-log-pause]");
    const clear = document.querySelector("[data-log-clear]");
    if (pause) pause.addEventListener("click", () => {
      paused = !paused;
      pause.textContent = paused ? "Resume" : "Pause";
      if (status) status.textContent = paused ? "Paused" : "Live";
    });
    if (clear) clear.addEventListener("click", () => { logView.replaceChildren(); });
    const addEntry = entry => {
      const line = document.createElement("div");
      line.className = "log-line";
      try {
        const parsed = JSON.parse(entry.line);
        const level = String(parsed.level || "").toLowerCase();
        if (level === "warn" || level === "error") line.classList.add(level);
      } catch (_) {}
      line.textContent = entry.line;
      logView.appendChild(line);
      after = Math.max(after, Number(entry.sequence || 0));
    };
    const poll = async () => {
      if (!paused) {
        try {
          const response = await fetch("/ui/api/logs?after=" + encodeURIComponent(after), {credentials:"same-origin", headers:{"Accept":"application/json"}});
          if (response.ok) {
            const payload = await response.json();
            (payload.entries || []).forEach(addEntry);
            if ((payload.entries || []).length) logView.scrollTop = logView.scrollHeight;
            if (status) status.textContent = "Live";
          } else if (status) status.textContent = "Disconnected";
        } catch (_) { if (status) status.textContent = "Disconnected"; }
      }
      setTimeout(poll, 1500);
    };
    poll();
  }
})();
`
