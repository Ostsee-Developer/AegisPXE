package webui

import (
	"fmt"
	"html/template"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/assignment"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/installation"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
	"github.com/Ostsee-Developer/AegisPXE/internal/operator"
)

const operatorSessionCookie = "aegispxe_operator_session"
const maxOperatorFormBody = 16 << 10

type operatorStore interface {
	SetMachinePolicy(rctx interface{ Done() <-chan struct{} }, machineID string, policy machine.Policy, requestID, actor string) (machine.Machine, error)
}

// mutationStore is intentionally separate from the read-only Store contract used by public Studio views.
type mutationStore interface {
	SetMachinePolicy(ctx interface{ Done() <-chan struct{} }, machineID string, policy machine.Policy, requestID, actor string) (machine.Machine, error)
	InstallationSpec(ctx interface{ Done() <-chan struct{} }, id string) (installation.Spec, error)
	ArmInstallation(ctx interface{ Done() <-chan struct{} }, machineID, installationID, requestID, actor string) (assignment.Assignment, error)
	CancelAssignment(ctx interface{ Done() <-chan struct{} }, installationID, requestID, actor string) (assignment.Assignment, error)
}

type operatorView struct {
	Available     bool
	Secure        bool
	Authenticated bool
	Actor         string
	CSRFToken     string
	ExpiresAt     time.Time
}

var operatorLoginTemplate = template.Must(template.New("operator-login").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>AegisPXE · Operator login</title><link rel="stylesheet" href="/ui/assets/style.css"></head>
<body><main class="login-shell"><section class="panel login-panel"><div class="panel-head"><div><p class="eyebrow">SECURE OPERATOR BOUNDARY</p><h2>Operator login</h2></div></div><div class="login-body">{{if .Secure}}<p class="muted">Enter the local bootstrap operator key. The key is never stored in the browser after the session is created.</p>{{if .Error}}<p class="auth-error">{{.Error}}</p>{{end}}<form method="post" action="/ui/operator/login"><label>Bootstrap operator key<input type="password" name="key" autocomplete="current-password" required></label><button type="submit">Authenticate</button></form>{{else}}<p class="auth-error">Operator login is disabled on cleartext network HTTP.</p><p class="muted">Use direct HTTPS or a loopback connection. Public Studio remains read-only.</p>{{end}}<p><a class="machine-link" href="/ui/">← Back to Studio</a></p></div></section></main></body></html>`))

func (u *UI) registerOperator(mux *http.ServeMux) {
	mux.HandleFunc("GET /ui/operator/login", u.operatorLoginPage)
	mux.HandleFunc("POST /ui/operator/login", u.operatorLogin)
	mux.HandleFunc("POST /ui/operator/logout", u.operatorLogout)
	mux.HandleFunc("POST /ui/operator/machines/{id}/policy", u.operatorMachinePolicy)
	mux.HandleFunc("POST /ui/operator/installations/{id}/arm", u.operatorArmInstallation)
	mux.HandleFunc("POST /ui/operator/installations/{id}/cancel", u.operatorCancelInstallation)
}

func (u *UI) operatorState(r *http.Request) operatorView {
	view := operatorView{Available: u.auth != nil, Secure: secureOperatorTransport(r)}
	if u.auth == nil || !view.Secure {
		return view
	}
	cookie, err := r.Cookie(operatorSessionCookie)
	if err != nil {
		return view
	}
	session, ok := u.auth.Session(cookie.Value)
	if !ok {
		return view
	}
	view.Authenticated = true
	view.Actor = session.Actor
	view.CSRFToken = session.CSRFToken
	view.ExpiresAt = session.ExpiresAt
	return view
}

func (u *UI) operatorLoginPage(w http.ResponseWriter, r *http.Request) {
	state := u.operatorState(r)
	if state.Authenticated {
		http.Redirect(w, r, "/ui/", http.StatusSeeOther)
		return
	}
	u.renderOperatorLogin(w, state.Secure, "")
}

func (u *UI) operatorLogin(w http.ResponseWriter, r *http.Request) {
	if u.auth == nil {
		http.NotFound(w, r)
		return
	}
	if !secureOperatorTransport(r) {
		u.logOperatorAuth(r, "login", fault.OperatorSecureTransportRequired, "rejected", "insecure_transport")
		http.Error(w, "operator login requires a secure transport", http.StatusUpgradeRequired)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxOperatorFormBody)
	if err := r.ParseForm(); err != nil {
		u.logOperatorAuth(r, "login", fault.OperatorAuthenticationFailed, "rejected", "invalid_form")
		u.renderOperatorLogin(w, true, "Invalid login request")
		return
	}
	token, session, err := u.auth.Login(operatorRemote(r), r.PostForm.Get("key"))
	if err != nil {
		code := fault.OperatorAuthenticationFailed
		cause := "invalid_key"
		if strings.Contains(err.Error(), "rate limit") {
			code = fault.OperatorAuthRateLimited
			cause = "rate_limited"
		}
		u.logOperatorAuth(r, "login", code, "rejected", cause)
		u.renderOperatorLogin(w, true, "Authentication failed")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     operatorSessionCookie,
		Value:    token,
		Path:     "/ui/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		Expires:  session.ExpiresAt,
		MaxAge:   int(operator.SessionDuration.Seconds()),
	})
	u.logOperatorAuth(r, "login", "", "success", "authenticated")
	http.Redirect(w, r, "/ui/", http.StatusSeeOther)
}

func (u *UI) operatorLogout(w http.ResponseWriter, r *http.Request) {
	session, token, ok := u.requireOperator(w, r)
	if !ok {
		return
	}
	u.auth.Logout(token)
	http.SetCookie(w, &http.Cookie{Name: operatorSessionCookie, Value: "", Path: "/ui/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	u.logger.InfoContext(r.Context(), "operator session ended", "component", "operator.auth", "operation", "logout", "request_id", operatorRequestID(r), "actor", session.Actor, "result", "success")
	http.Redirect(w, r, "/ui/", http.StatusSeeOther)
}

func (u *UI) operatorMachinePolicy(w http.ResponseWriter, r *http.Request) {
	session, _, ok := u.requireOperator(w, r)
	if !ok {
		return
	}
	store, ok := u.store.(interface {
		SetMachinePolicy(context.Context, string, machine.Policy, string, string) (machine.Machine, error)
	})
	if !ok {
		u.renderError(w, r, "operator machine policy", fmt.Errorf("store does not implement machine policy mutation"))
		return
	}
	policy := machine.Policy(strings.TrimSpace(r.PostForm.Get("policy")))
	machineID := strings.TrimSpace(r.PathValue("id"))
	if _, err := store.SetMachinePolicy(r.Context(), machineID, policy, operatorRequestID(r), session.Actor); err != nil {
		u.renderError(w, r, "operator machine policy", err)
		return
	}
	http.Redirect(w, r, "/ui/machines/"+machineID, http.StatusSeeOther)
}

func (u *UI) operatorArmInstallation(w http.ResponseWriter, r *http.Request) {
	session, _, ok := u.requireOperator(w, r)
	if !ok {
		return
	}
	store, ok := u.store.(interface {
		InstallationSpec(context.Context, string) (installation.Spec, error)
		ArmInstallation(context.Context, string, string, string, string) (assignment.Assignment, error)
	})
	if !ok {
		u.renderError(w, r, "operator arm installation", fmt.Errorf("store does not implement installation arming"))
		return
	}
	installationID := strings.TrimSpace(r.PathValue("id"))
	spec, err := store.InstallationSpec(r.Context(), installationID)
	if err != nil {
		u.renderError(w, r, "operator arm installation", err)
		return
	}
	if _, err := store.ArmInstallation(r.Context(), spec.MachineID, spec.ID, operatorRequestID(r), session.Actor); err != nil {
		u.renderError(w, r, "operator arm installation", err)
		return
	}
	http.Redirect(w, r, "/ui/installations/"+installationID, http.StatusSeeOther)
}

func (u *UI) operatorCancelInstallation(w http.ResponseWriter, r *http.Request) {
	session, _, ok := u.requireOperator(w, r)
	if !ok {
		return
	}
	store, ok := u.store.(interface {
		CancelAssignment(context.Context, string, string, string) (assignment.Assignment, error)
	})
	if !ok {
		u.renderError(w, r, "operator cancel installation", fmt.Errorf("store does not implement assignment cancellation"))
		return
	}
	installationID := strings.TrimSpace(r.PathValue("id"))
	if _, err := store.CancelAssignment(r.Context(), installationID, operatorRequestID(r), session.Actor); err != nil {
		u.renderError(w, r, "operator cancel installation", err)
		return
	}
	http.Redirect(w, r, "/ui/installations/"+installationID, http.StatusSeeOther)
}

func (u *UI) requireOperator(w http.ResponseWriter, r *http.Request) (operator.Session, string, bool) {
	if u.auth == nil {
		http.NotFound(w, r)
		return operator.Session{}, "", false
	}
	if !secureOperatorTransport(r) {
		u.logOperatorAuth(r, "authorize", fault.OperatorSecureTransportRequired, "rejected", "insecure_transport")
		http.Error(w, "operator action requires a secure transport", http.StatusUpgradeRequired)
		return operator.Session{}, "", false
	}
	cookie, err := r.Cookie(operatorSessionCookie)
	if err != nil {
		u.logOperatorAuth(r, "authorize", fault.OperatorSessionRequired, "rejected", "missing_session")
		http.Error(w, "operator session required", http.StatusUnauthorized)
		return operator.Session{}, "", false
	}
	session, ok := u.auth.Session(cookie.Value)
	if !ok {
		u.logOperatorAuth(r, "authorize", fault.OperatorSessionRequired, "rejected", "invalid_or_expired_session")
		http.Error(w, "operator session required", http.StatusUnauthorized)
		return operator.Session{}, "", false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxOperatorFormBody)
	if err := r.ParseForm(); err != nil || !u.auth.ValidateCSRF(session, r.PostForm.Get("csrf")) {
		u.logOperatorAuth(r, "authorize", fault.OperatorCSRFInvalid, "rejected", "csrf_invalid")
		http.Error(w, "invalid operator request", http.StatusForbidden)
		return operator.Session{}, "", false
	}
	return session, cookie.Value, true
}

func (u *UI) renderOperatorLogin(w http.ResponseWriter, secure bool, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = operatorLoginTemplate.Execute(w, struct {
		Secure bool
		Error  string
	}{Secure: secure, Error: message})
}

func (u *UI) logOperatorAuth(r *http.Request, operation, code, result, cause string) {
	u.logger.WarnContext(r.Context(), "operator authentication decision", "component", "operator.auth", "operation", operation, "request_id", operatorRequestID(r), "remote", operatorRemote(r), "error_code", code, "result", result, "cause", cause)
}

func secureOperatorTransport(r *http.Request) bool {
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

func operatorRemote(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	if value := strings.TrimSpace(r.RemoteAddr); value != "" {
		return value
	}
	return "unknown"
}

func operatorRequestID(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Request-ID"))
}
