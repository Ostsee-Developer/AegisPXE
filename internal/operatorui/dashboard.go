package operatorui

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
	"github.com/Ostsee-Developer/AegisPXE/internal/observability"
	"github.com/Ostsee-Developer/AegisPXE/internal/operator"
	"github.com/Ostsee-Developer/AegisPXE/internal/operatorpasskey"
	"github.com/Ostsee-Developer/AegisPXE/internal/store"
)

type DashboardHandler struct {
	next     http.Handler
	state    *store.Store
	auth     *operator.Manager
	passkeys *operatorpasskey.Service
	logs     *observability.LogBuffer
	logger   *slog.Logger
	resolver debianArtifactResolver
	mux      *http.ServeMux
}

func NewDashboard(next http.Handler, state *store.Store, auth *operator.Manager, passkeys *operatorpasskey.Service, logs *observability.LogBuffer, logger *slog.Logger) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	if logger == nil {
		logger = slog.Default()
	}
	h := &DashboardHandler{
		next:     next,
		state:    state,
		auth:     auth,
		passkeys: passkeys,
		logs:     logs,
		logger:   logger,
		resolver: newDebianArtifactResolver(logger),
		mux:      http.NewServeMux(),
	}
	h.registerDashboardRoutes()
	return h
}

func NewDashboardWithTrustedProxy(next http.Handler, state *store.Store, auth *operator.Manager, passkeys *operatorpasskey.Service, logs *observability.LogBuffer, logger *slog.Logger, proxy TrustedProxy) http.Handler {
	base := NewDashboard(next, state, auth, passkeys, logs, logger)
	if !proxy.Enabled() {
		return base
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &trustedProxyIdentityHandler{next: base, logger: logger, proxy: proxy}
}

func (h *DashboardHandler) registerDashboardRoutes() {
	h.mux.HandleFunc("GET /ui/{$}", h.dashboardEntry)
	h.mux.HandleFunc("GET /ui/assets/dashboard.css", h.dashboardStableStyle)
	h.mux.HandleFunc("GET /ui/assets/dashboard.js", h.dashboardStableScript)

	h.mux.HandleFunc("POST /ui/auth/bootstrap", h.bootstrapInitialAdmin)
	h.mux.HandleFunc("POST /ui/api/passkey/login/start", h.beginExternalPasskeyLogin)
	h.mux.HandleFunc("POST /ui/api/passkey/login/finish", h.finishExternalPasskeyLogin)
	h.mux.HandleFunc("POST /ui/api/passkey/enroll/start", h.beginPasskeyEnrollment)
	h.mux.HandleFunc("POST /ui/api/passkey/enroll/finish", h.finishPasskeyEnrollment)
	h.mux.HandleFunc("POST /ui/api/recovery/start", h.beginRecoveryPasskey)
	h.mux.HandleFunc("POST /ui/api/recovery/passkey", h.finishRecoveryPasskey)
	h.mux.HandleFunc("POST /ui/recovery/finish", h.finishRecoveryLogin)
	h.mux.HandleFunc("POST /ui/logout", h.dashboardLogout)

	h.mux.HandleFunc("GET /ui/machines", h.dashboardMachines)
	h.mux.HandleFunc("GET /ui/machines/{id}", h.dashboardMachine)
	h.mux.HandleFunc("GET /ui/machines/{id}/manage", h.dashboardMachineManagement)
	h.mux.HandleFunc("POST /ui/machines/{id}/policy", h.dashboardMachinePolicy)
	h.mux.HandleFunc("POST /ui/machines/{id}/nickname", h.dashboardMachineNickname)
	h.mux.HandleFunc("POST /ui/machines/{id}/delete", h.dashboardDeleteMachine)
	h.mux.HandleFunc("GET /ui/api/machine-metadata", h.dashboardMachineMetadata)

	h.mux.HandleFunc("GET /ui/installations", h.dashboardInstallations)
	h.mux.HandleFunc("GET /ui/installations/new", h.dashboardInstallationWizard)
	h.mux.HandleFunc("POST /ui/installations", h.dashboardCreateInstallation)
	h.mux.HandleFunc("GET /ui/installations/{id}", h.dashboardInstallation)
	h.mux.HandleFunc("GET /ui/installations/{id}/manage", h.dashboardInstallationManagement)
	h.mux.HandleFunc("POST /ui/installations/{id}/arm", h.dashboardArmInstallation)
	h.mux.HandleFunc("POST /ui/installations/{id}/cancel", h.dashboardCancelInstallation)
	h.mux.HandleFunc("POST /ui/installations/{id}/delete", h.dashboardDeleteInstallation)
	h.mux.HandleFunc("GET /ui/installations/{id}/trust", h.dashboardInstallationTrust)
	h.mux.HandleFunc("POST /ui/installations/{id}/trust/{fingerprint}/approve", h.dashboardApproveBootTrustKey)
	h.mux.HandleFunc("POST /ui/installations/{id}/trust/{fingerprint}/revoke", h.dashboardRevokeBootTrustKey)

	h.mux.HandleFunc("GET /ui/users", h.dashboardUsers)
	h.mux.HandleFunc("POST /ui/users/{id}/approve", h.dashboardApproveUser)
	h.mux.HandleFunc("POST /ui/users/{id}/block", h.dashboardBlockUser)

	h.mux.HandleFunc("GET /ui/logs", h.dashboardLogs)
	h.mux.HandleFunc("GET /ui/api/logs", h.dashboardStableLogFeed)
	h.mux.HandleFunc("GET /ui/api/logs/tail", h.dashboardLogTail)
	h.mux.HandleFunc("GET /ui/logs/export", h.dashboardStableLogExport)

	// Old dev.4/dev.8 routes intentionally collapse into the single dashboard.
	h.mux.HandleFunc("GET /ui/operator/{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusPermanentRedirect)
	})
	h.mux.HandleFunc("GET /ui/operator/login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusPermanentRedirect)
	})
	h.mux.HandleFunc("GET /ui/operator/installations/new", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/installations/new", http.StatusPermanentRedirect)
	})
}

func (h *DashboardHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/ui" {
		http.Redirect(w, r, "/ui/", http.StatusPermanentRedirect)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/ui/") {
		h.next.ServeHTTP(w, r)
		return
	}

	requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
	if requestID == "" || len(requestID) > 128 {
		var err error
		requestID, err = idgen.New("req_")
		if err != nil {
			h.logger.ErrorContext(r.Context(), "dashboard request correlation failed",
				"component", "operator.http",
				"operation", "request_id",
				"error_code", fault.StorageFailure,
				"result", "failure",
				"cause", err.Error(),
			)
			http.Error(w, "dashboard unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	r.Header.Set("X-Request-ID", requestID)
	w.Header().Set("X-Request-ID", requestID)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'; img-src 'self' data:; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=(), serial=()")
	w.Header().Set("Cache-Control", "no-store")
	h.mux.ServeHTTP(w, r)
}

func (h *DashboardHandler) dashboardSession(r *http.Request) (operator.Session, string, bool) {
	if h.auth == nil || h.state == nil || !secureTransport(r) {
		return operator.Session{}, "", false
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return operator.Session{}, "", false
	}
	session, ok := h.auth.Session(cookie.Value)
	if !ok || strings.TrimSpace(session.UserID) == "" {
		return operator.Session{}, "", false
	}
	user, err := h.state.OperatorUser(r.Context(), session.UserID)
	if err != nil || user.Status != "active" || user.Role != session.Role || session.Actor != "user:"+user.Subject {
		h.auth.Logout(cookie.Value)
		cause := "principal_state_changed"
		if err != nil {
			cause = "principal_unavailable"
		}
		h.logger.WarnContext(r.Context(), "dashboard session invalidated after principal revalidation",
			"component", "operator.auth",
			"operation", "session_revalidate",
			"request_id", requestID(r),
			"user_id", session.UserID,
			"actor", session.Actor,
			"auth_method", session.AuthMethod,
			"error_code", fault.OperatorSessionRequired,
			"result", "rejected",
			"cause", cause,
		)
		return operator.Session{}, "", false
	}
	return session, cookie.Value, true
}

func (h *DashboardHandler) requireDashboardPage(w http.ResponseWriter, r *http.Request) (operator.Session, bool) {
	session, _, ok := h.dashboardSession(r)
	if !ok {
		http.Redirect(w, r, "/ui/", http.StatusSeeOther)
		return operator.Session{}, false
	}
	return session, true
}

func (h *DashboardHandler) requireDashboardAction(w http.ResponseWriter, r *http.Request, admin bool) (operator.Session, bool) {
	if !secureTransport(r) {
		h.dashboardAuthRejected(r, "authorize", fault.OperatorSecureTransportRequired, "insecure_transport")
		http.Error(w, "secure transport required", http.StatusUpgradeRequired)
		return operator.Session{}, false
	}
	session, _, ok := h.dashboardSession(r)
	if !ok {
		h.dashboardAuthRejected(r, "authorize", fault.OperatorSessionRequired, "missing_or_expired_session")
		http.Error(w, "dashboard session required", http.StatusUnauthorized)
		return operator.Session{}, false
	}
	if admin && !session.IsAdmin() {
		h.dashboardAuthRejected(r, "authorize", fault.OperatorAuthorizationDenied, "admin_role_required")
		http.Error(w, "administrator role required", http.StatusForbidden)
		return operator.Session{}, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBody)
	if err := r.ParseForm(); err != nil || !h.auth.ValidateCSRF(session, r.PostForm.Get("csrf")) {
		h.dashboardAuthRejected(r, "authorize", fault.OperatorCSRFInvalid, "csrf_invalid")
		http.Error(w, "invalid dashboard request", http.StatusForbidden)
		return operator.Session{}, false
	}
	return session, true
}

func (h *DashboardHandler) dashboardAuthRejected(r *http.Request, operation, code, cause string) {
	h.logger.WarnContext(r.Context(), "dashboard authentication decision",
		"component", "operator.auth",
		"operation", operation,
		"request_id", requestID(r),
		"remote", remoteHost(r),
		"error_code", code,
		"result", "rejected",
		"cause", cause,
	)
}

func (h *DashboardHandler) setDashboardSessionCookie(w http.ResponseWriter, r *http.Request, token string, session operator.Session) {
	// Trusted-proxy requests are cloned with synthetic TLS state before they
	// reach the Dashboard, so r.TLS!=nil correctly marks the browser-facing
	// HTTPS path. Loopback recovery is intentionally allowed over local HTTP;
	// marking that cookie Secure would make browsers refuse to send it back.
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
}

func (h *DashboardHandler) clearDashboardSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/ui/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}
