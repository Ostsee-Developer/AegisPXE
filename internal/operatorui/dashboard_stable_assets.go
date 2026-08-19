package operatorui

import "net/http"

func (h *DashboardHandler) dashboardStableStyle(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, max-age=0, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	_, _ = w.Write([]byte(dashboardCSS + dashboardRCTheme + dashboardManagementCSS))
}

func (h *DashboardHandler) dashboardStableScript(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, max-age=0, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	_, _ = w.Write([]byte(dashboardJS + dashboardRCJS + dashboardManagementJS))
}

const dashboardManagementCSS = `
.machine-id-sub{display:block;color:var(--muted);font-size:10px;margin-top:2px}
.management-shell{min-height:100vh;padding:34px 20px;background:var(--bg)}
.management-card{width:min(100%,980px);margin:0 auto;background:var(--surface);border:1px solid var(--border);border-radius:var(--radius);box-shadow:var(--shadow);overflow:hidden}
.management-card .page-head{padding:22px 24px 18px;margin:0}
.management-card .detail-body{padding:22px 24px}
.management-card .section+.section{margin-top:28px;padding-top:22px;border-top:1px solid var(--border)}
.management-card label{display:grid;gap:8px;margin-top:14px}
.management-card input{width:100%;min-height:42px;padding:9px 11px}
.management-card .card-actions{margin-top:14px}
.secureboot-chip{display:inline-flex;align-items:center;gap:7px;margin-top:9px;padding:5px 9px;border:1px solid var(--border);border-radius:999px;color:var(--muted);font-size:11px;letter-spacing:.02em}
.secureboot-chip strong{color:var(--text);font-size:11px}
.secureboot-dot{width:7px;height:7px;border-radius:50%;background:currentColor}
.secureboot-chip[data-state="enabled"]{color:var(--accent)}
.secureboot-chip[data-state="disabled"],.secureboot-chip[data-state="setup_mode"]{color:var(--danger)}
`

const dashboardManagementJS = `
(() => {
  "use strict";

  const addManageLink = (href, label) => {
    const head = document.querySelector("main.content .page-head");
    if (!head || head.querySelector("[data-aegis-manage-link]")) return;
    let actions = head.querySelector(".actions");
    if (!actions) {
      actions = document.createElement("div");
      actions.className = "actions";
      head.appendChild(actions);
    }
    const link = document.createElement("a");
    link.className = "button secondary";
    link.href = href;
    link.textContent = label;
    link.dataset.aegisManageLink = "1";
    actions.appendChild(link);
  };

  const machineMatch = location.pathname.match(/^\/ui\/machines\/([^/]+)$/);
  if (machineMatch) addManageLink(location.pathname + "/manage", "Manage machine");
  const installationMatch = location.pathname.match(/^\/ui\/installations\/([^/]+)$/);
  if (installationMatch) addManageLink(location.pathname + "/manage", "Manage installation");

  fetch("/ui/api/machine-metadata", {credentials:"same-origin", headers:{"Accept":"application/json"}})
    .then(response => response.ok ? response.json() : Promise.reject(new Error("metadata unavailable")))
    .then(payload => {
      const names = payload.machines || {};
      document.querySelectorAll('a[href^="/ui/machines/"]').forEach(link => {
        const match = link.getAttribute("href").match(/^\/ui\/machines\/([^/]+)$/);
        if (!match) return;
        const nickname = String((names[match[1]] || {}).nickname || "").trim();
        if (!nickname || link.dataset.nicknameDecorated) return;
        link.dataset.nicknameDecorated = "1";
        const id = link.textContent.trim();
        link.textContent = nickname;
        link.title = id;
        const suffix = document.createElement("small");
        suffix.className = "mono machine-id-sub";
        suffix.textContent = id;
        link.appendChild(suffix);
      });
      if (machineMatch) {
        const metadata = names[machineMatch[1]] || {};
        const heading = document.querySelector(".detail-panel .panel-head h2.mono");
        const nickname = String(metadata.nickname || "").trim();
        if (heading && nickname) {
          const id = heading.textContent.trim();
          heading.classList.remove("mono");
          heading.textContent = nickname;
          const idLine = document.createElement("p");
          idLine.className = "mono";
          idLine.textContent = id;
          heading.insertAdjacentElement("afterend", idLine);
        }
        const state = String(metadata.secure_boot_state || "unknown").trim() || "unknown";
        const panelHead = document.querySelector(".detail-panel .panel-head > div");
        if (panelHead && !panelHead.querySelector("[data-secureboot-chip]")) {
          const chip = document.createElement("span");
          chip.className = "secureboot-chip";
          chip.dataset.securebootChip = "1";
          chip.dataset.state = state;
          const dot = document.createElement("span");
          dot.className = "secureboot-dot";
          const label = document.createElement("span");
          label.textContent = "Secure Boot ";
          const value = document.createElement("strong");
          value.textContent = state.toUpperCase().replaceAll("_", " ");
          label.appendChild(value);
          chip.append(dot, label);
          panelHead.appendChild(chip);
        }
      }
    })
    .catch(() => {});
})();
`
