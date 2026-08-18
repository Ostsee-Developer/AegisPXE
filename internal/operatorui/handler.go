package operatorui

import (
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
	"github.com/Ostsee-Developer/AegisPXE/internal/operator"
	"github.com/Ostsee-Developer/AegisPXE/internal/store"
)

const (
	sessionCookieName = "aegispxe_operator_session"
	maxFormBody       = 96 << 10
)

type Handler struct {
	next     http.Handler
	state    *store.Store
	auth     *operator.Manager
	logger   *slog.Logger
	mux      *http.ServeMux
	resolver debianArtifactResolver
}

type loginView struct {
	Secure bool
	Error  string
}

var loginTemplate = template.Must(template.New("operator-login").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>AegisPXE · Operator login</title>
  <link rel="stylesheet" href="/ui/assets/style.css">
  <link rel="stylesheet" href="/ui/operator/assets/operator.css">
</head>
<body>
<main class="operator-login-shell">
  <section class="panel operator-login-panel">
    <div class="panel-head"><div><p class="eyebrow">SECURE OPERATOR BOUNDARY</p><h2>Operator login</h2></div></div>
    <div class="operator-login-body">
      {{if .Secure}}
      <p class="muted">Enter the local bootstrap operator key. AegisPXE exchanges it for a short-lived server-side session and never stores the key in the browser.</p>
      {{if .Error}}<p class="operator-auth-error">{{.Error}}</p>{{end}}
      <form method="post" action="/ui/operator/login">
        <label>Bootstrap operator key<input type="password" name="key" autocomplete="current-password" required></label>
        <button class="op-button" type="submit">Authenticate</button>
      </form>
      {{else}}
      <p class="operator-auth-error">Operator login is disabled on cleartext network HTTP.</p>
      <p class="muted">Use the loopback operator listener through an SSH tunnel or a direct HTTPS connection. Public Studio remains read-only.</p>
      {{end}}
      <p><a class="machine-link" href="/ui/">← Back to Studio</a></p>
    </div>
  </section>
</main>
</body>
</html>`))

func New(next http.Handler, state *store.Store, auth *operator.Manager, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &Handler{
		next:     next,
		state:    state,
		auth:     auth,
		logger:   logger,
		mux:      http.NewServeMux(),
		resolver: newDebianArtifactResolver(logger),
	}
	h.mux.HandleFunc("GET /ui/operator/login", h.loginPage)
	h.mux.HandleFunc("POST /ui/operator/login", h.login)
	h.mux.HandleFunc("POST /ui/operator/logout", h.logout)
	h.mux.HandleFunc("POST /ui/operator/machines/{id}/policy", h.machinePolicy)
	h.mux.HandleFunc("POST /ui/operator/installations/{id}/arm", h.armInstallation)
	h.mux.HandleFunc("POST /ui/operator/installations/{id}/cancel", h.cancelInstallation)
	h.registerConsole()
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/ui/operator/") {
		h.next.ServeHTTP(w, r)
		return
	}
	requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
	if requestID == "" || len(requestID) > 128 {
		var err error
		requestID, err = idgen.New("req_")
		if err != nil {
			h.logger.ErrorContext(r.Context(), "operator request correlation failed", "component", "operator.http", "operation", "request_id", "error_code", fault.StorageFailure, "error", err)
			http.Error(w, "operator service unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	r.Header.Set("X-Request-ID", requestID)
	w.Header().Set("X-Request-ID", requestID)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	w.Header().Set("Cache-Control", "no-store")
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) loginPage(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		http.NotFound(w, r)
		return
	}
	if _, ok := h.session(r); ok {
		http.Redirect(w, r, "/ui/operator/", http.StatusSeeOther)
		return
	}
	h.renderLogin(w, secureTransport(r), "")
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		http.NotFound(w, r)
		return
	}
	if !secureTransport(r) {
		h.logRejected(r, "login", fault.OperatorSecureTransportRequired, "insecure_transport")
		http.Error(w, "operator login requires secure transport", http.StatusUpgradeRequired)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBody)
	if err := r.ParseForm(); err != nil {
		h.logRejected(r, "login", fault.OperatorAuthenticationFailed, "invalid_form")
		h.renderLogin(w, true, "Authentication failed")
		return
	}
	token, session, err := h.auth.Login(remoteHost(r), r.PostForm.Get("key"))
	if err != nil {
		code := fault.OperatorAuthenticationFailed
		cause := "invalid_key"
		if strings.Contains(err.Error(), "rate limit") {
			code = fault.OperatorAuthRateLimited
			cause = "rate_limited"
		}
		h.logRejected(r, "login", code, cause)
		h.renderLogin(w, true, "Authentication failed")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/ui/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		Expires:  session.ExpiresAt,
		MaxAge:   int(operator.SessionDuration.Seconds()),
	})
	h.logger.InfoContext(r.Context(), "operator authenticated",
		"component", "operator.auth",
		"operation", "login",
		"request_id", requestID(r),
		"remote", remoteHost(r),
		"actor", session.Actor,
		"expires_at", session.ExpiresAt,
		"result", "success",
	)
	http.Redirect(w, r, "/ui/operator/", http.StatusSeeOther)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	session, token, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	h.auth.Logout(token)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/ui/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	h.logger.InfoContext(r.Context(), "operator session ended",
		"component", "operator.auth",
		"operation", "logout",
		"request_id", requestID(r),
		"actor", session.Actor,
		"result", "success",
	)
	http.Redirect(w, r, "/ui/", http.StatusSeeOther)
}

func (h *Handler) machinePolicy(w http.ResponseWriter, r *http.Request) {
	session, _, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	machineID := strings.TrimSpace(r.PathValue("id"))
	policy := machine.Policy(strings.TrimSpace(r.PostForm.Get("policy")))
	if _, err := h.state.SetMachinePolicy(r.Context(), machineID, policy, requestID(r), session.Actor); err != nil {
		h.writeMutationError(w, r, "machine_policy", err)
		return
	}
	http.Redirect(w, r, "/ui/operator/", http.StatusSeeOther)
}

func (h *Handler) armInstallation(w http.ResponseWriter, r *http.Request) {
	session, _, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	installationID := strings.TrimSpace(r.PathValue("id"))
	spec, err := h.state.InstallationSpec(r.Context(), installationID)
	if err != nil {
		h.writeMutationError(w, r, "arm_installation", err)
		return
	}
	if _, err := h.state.ArmInstallation(r.Context(), spec.MachineID, spec.ID, requestID(r), session.Actor); err != nil {
		h.writeMutationError(w, r, "arm_installation", err)
		return
	}
	http.Redirect(w, r, "/ui/installations/"+installationID, http.StatusSeeOther)
}

func (h *Handler) cancelInstallation(w http.ResponseWriter, r *http.Request) {
	session, _, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	installationID := strings.TrimSpace(r.PathValue("id"))
	if _, err := h.state.CancelAssignment(r.Context(), installationID, requestID(r), session.Actor); err != nil {
		h.writeMutationError(w, r, "cancel_installation", err)
		return
	}
	http.Redirect(w, r, "/ui/installations/"+installationID, http.StatusSeeOther)
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (operator.Session, string, bool) {
	if h.auth == nil {
		http.NotFound(w, r)
		return operator.Session{}, "", false
	}
	if !secureTransport(r) {
		h.logRejected(r, "authorize", fault.OperatorSecureTransportRequired, "insecure_transport")
		http.Error(w, "operator action requires secure transport", http.StatusUpgradeRequired)
		return operator.Session{}, "", false
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		h.logRejected(r, "authorize", fault.OperatorSessionRequired, "missing_session")
		http.Error(w, "operator session required", http.StatusUnauthorized)
		return operator.Session{}, "", false
	}
	session, ok := h.auth.Session(cookie.Value)
	if !ok {
		h.logRejected(r, "authorize", fault.OperatorSessionRequired, "invalid_or_expired_session")
		http.Error(w, "operator session required", http.StatusUnauthorized)
		return operator.Session{}, "", false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBody)
	if err := r.ParseForm(); err != nil || !h.auth.ValidateCSRF(session, r.PostForm.Get("csrf")) {
		h.logRejected(r, "authorize", fault.OperatorCSRFInvalid, "csrf_invalid")
		http.Error(w, "invalid operator request", http.StatusForbidden)
		return operator.Session{}, "", false
	}
	h.logger.InfoContext(r.Context(), "operator action authorized",
		"component", "operator.auth",
		"operation", "authorize",
		"request_id", requestID(r),
		"actor", session.Actor,
		"result", "success",
	)
	return session, cookie.Value, true
}

func (h *Handler) session(r *http.Request) (operator.Session, bool) {
	if h.auth == nil || !secureTransport(r) {
		return operator.Session{}, false
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return operator.Session{}, false
	}
	return h.auth.Session(cookie.Value)
}

func (h *Handler) writeMutationError(w http.ResponseWriter, r *http.Request, operation string, err error) {
	code := fault.Code(err)
	status := http.StatusBadRequest
	switch code {
	case fault.MachineNotFound, fault.InstallationNotFound, fault.InstallationAssignmentNotFound:
		status = http.StatusNotFound
	case fault.InstallationAssignmentConflict:
		status = http.StatusConflict
	case fault.StorageFailure:
		status = http.StatusServiceUnavailable
	}
	if code == "" {
		code = fault.StorageFailure
		status = http.StatusInternalServerError
	}
	h.logger.WarnContext(r.Context(), "operator mutation failed",
		"component", "operator.http",
		"operation", operation,
		"request_id", requestID(r),
		"error_code", code,
		"result", "failure",
		"cause", err.Error(),
	)
	http.Error(w, "operator mutation failed: "+code, status)
}

func (h *Handler) renderLogin(w http.ResponseWriter, secure bool, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = loginTemplate.Execute(w, loginView{Secure: secure, Error: message})
}

func (h *Handler) logRejected(r *http.Request, operation, code, cause string) {
	h.logger.WarnContext(r.Context(), "operator authentication decision",
		"component", "operator.auth",
		"operation", operation,
		"request_id", requestID(r),
		"remote", remoteHost(r),
		"error_code", code,
		"result", "rejected",
		"cause", cause,
	)
}

func secureTransport(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	if value := strings.TrimSpace(r.RemoteAddr); value != "" {
		return value
	}
	return "unknown"
}

func requestID(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Request-ID"))
}
