package operatorui

import "net/http"

func (h *DashboardHandler) dashboardStableScript(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, max-age=0, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	_, _ = w.Write([]byte(dashboardJS + dashboardRCJS + dashboardManagementJS))
}

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
        suffix.className = "mono";
        suffix.style.display = "block";
        suffix.style.color = "var(--muted)";
        suffix.textContent = id;
        link.appendChild(suffix);
      });
      if (machineMatch) {
        const heading = document.querySelector(".detail-panel .panel-head h2.mono");
        const nickname = String((names[machineMatch[1]] || {}).nickname || "").trim();
        if (heading && nickname) {
          const id = heading.textContent.trim();
          heading.classList.remove("mono");
          heading.textContent = nickname;
          const idLine = document.createElement("p");
          idLine.className = "mono";
          idLine.textContent = id;
          heading.insertAdjacentElement("afterend", idLine);
        }
      }
    })
    .catch(() => {});
})();
`
