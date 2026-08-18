package operatorui

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/operatoridentity"
	"github.com/Ostsee-Developer/AegisPXE/internal/operatorpasskey"
)

type authView struct {
	Title       string
	Description string
	Subject     string
	State       string
	CanPasskey  bool
}

var dashboardAuthTemplate = template.Must(template.New("dashboard-auth").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>AegisPXE · {{.Title}}</title><link rel="stylesheet" href="/ui/assets/dashboard.css"></head>
<body><main class="auth-shell"><section class="auth-card"><div class="brand"><span class="brand-mark">A</span><span class="brand-copy"><strong>AegisPXE</strong><span>Secure provisioning control plane</span></span></div><p class="eyebrow">DOUBLE AUTHENTICATION BOUNDARY</p><h1>{{.Title}}</h1><p class="muted">{{.Description}}</p><div class="security-chain"><div class="security-step">Outer identity <strong>{{if .Subject}}{{.Subject}}{{else}}Recovery{{end}}</strong></div><div class="security-step">AegisPXE passkey <strong>{{if .CanPasskey}}Required{{else}}Unavailable{{end}}</strong></div></div><div data-auth-message hidden></div>
{{if eq .State "bootstrap"}}<form method="post" action="/ui/auth/bootstrap"><label>Initial administrator recovery key<input type="password" name="key" autocomplete="off" required><small>This one-time bootstrap factor is verified locally by AegisPXE.</small></label><button class="button" type="submit">Claim initial administrator</button></form>
{{else if eq .State "pending"}}<div class="notice warn">Your identity is registered and waiting for administrator review.</div>
{{else if eq .State "enroll"}}<div class="auth-actions"><button class="button" type="button" data-passkey-enroll {{if not .CanPasskey}}disabled{{end}}>Enroll AegisPXE passkey</button></div>
{{else if eq .State "login"}}<div class="auth-actions"><button class="button" type="button" data-passkey-login {{if not .CanPasskey}}disabled{{end}}>Continue with passkey</button></div>
{{else if eq .State "blocked"}}<div class="notice danger">This AegisPXE account is blocked.</div>
{{else if eq .State "recovery"}}<div class="notice">Zoraxy identity is unavailable. Emergency access requires all three factors: username, AegisPXE passkey and recovery key.</div><div class="auth-actions"><label>AegisPXE username<input name="recovery_subject" autocomplete="username" required></label><button class="button" type="button" data-recovery-passkey {{if not .CanPasskey}}disabled{{end}}>Verify recovery passkey</button></div><form method="post" action="/ui/recovery/finish" data-recovery-key-form hidden><input type="hidden" name="subject"><input type="hidden" name="ticket"><label>Recovery key<input type="password" name="key" autocomplete="off" required></label><button class="button" type="submit">Enter emergency session</button></form>
{{else}}<div class="notice danger">Authentication is unavailable.</div>{{end}}
{{if not .CanPasskey}}<p class="notice warn">WebAuthn is not configured on this AegisPXE instance.</p>{{end}}</section></main><script src="/ui/assets/dashboard.js" defer></script></body></html>`))

func (h *DashboardHandler) dashboardEntry(w http.ResponseWriter, r *http.Request) {
	if session, _, ok := h.dashboardSession(r); ok {
		h.renderDashboardOverview(w, r, session)
		return
	}
	identity, hasIdentity := externalIdentityFromRequest(r)
	if !hasIdentity {
		h.renderAuth(w, authView{Title: "Emergency access", Description: "No trusted upstream identity was supplied.", State: "recovery", CanPasskey: h.passkeys != nil})
		return
	}
	user, _, err := h.state.ResolveOperatorUser(r.Context(), identity.Provider, identity.Subject, identity.Subject, "", requestID(r))
	if err != nil {
		h.dashboardAuthRejected(r, "resolve_identity", fault.OperatorAuthenticationFailed, "identity_resolution_failed")
		h.renderAuth(w, authView{Title: "Authentication unavailable", Description: "AegisPXE could not resolve this identity.", State: "error", Subject: identity.Subject, CanPasskey: h.passkeys != nil})
		return
	}
	switch user.Status {
	case operatoridentity.StatusPendingReview:
		hasAdmin, err := h.state.HasOperatorAdmin(r.Context())
		if err != nil {
			h.renderAuth(w, authView{Title: "Authentication unavailable", Description: "AegisPXE could not verify bootstrap state.", State: "error", Subject: user.Subject, CanPasskey: h.passkeys != nil})
			return
		}
		if !hasAdmin {
			h.renderAuth(w, authView{Title: "Create initial administrator", Description: "Your trusted identity is the first AegisPXE user. Confirm the local recovery key before passkey enrollment.", State: "bootstrap", Subject: user.Subject, CanPasskey: h.passkeys != nil})
			return
		}
		h.renderAuth(w, authView{Title: "Pending review", Description: "An AegisPXE administrator must approve this identity before passkey enrollment.", State: "pending", Subject: user.Subject, CanPasskey: h.passkeys != nil})
	case operatoridentity.StatusEnrollmentRequired:
		h.renderAuth(w, authView{Title: "Enroll your passkey", Description: "Administrator approval is complete. Register the second AegisPXE authentication factor.", State: "enroll", Subject: user.Subject, CanPasskey: h.passkeys != nil})
	case operatoridentity.StatusActive:
		h.renderAuth(w, authView{Title: "AegisPXE passkey", Description: "The outer identity is accepted. Complete the independent AegisPXE passkey challenge.", State: "login", Subject: user.Subject, CanPasskey: h.passkeys != nil})
	case operatoridentity.StatusBlocked:
		h.renderAuth(w, authView{Title: "Access blocked", Description: "The outer identity is valid, but AegisPXE authorization denies access.", State: "blocked", Subject: user.Subject, CanPasskey: h.passkeys != nil})
	default:
		h.renderAuth(w, authView{Title: "Authentication unavailable", Description: "AegisPXE rejected this account state.", State: "error", Subject: user.Subject, CanPasskey: h.passkeys != nil})
	}
}

func (h *DashboardHandler) bootstrapInitialAdmin(w http.ResponseWriter, r *http.Request) {
	identity, ok := externalIdentityFromRequest(r)
	if !ok || !secureTransport(r) {
		http.Error(w, "trusted identity required", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBody)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid bootstrap request", http.StatusBadRequest)
		return
	}
	user, _, err := h.state.ResolveOperatorUser(r.Context(), identity.Provider, identity.Subject, identity.Subject, "", requestID(r))
	if err != nil || user.Status != operatoridentity.StatusPendingReview {
		http.Error(w, "initial administrator claim rejected", http.StatusForbidden)
		return
	}
	if err := h.auth.VerifyRecoveryKey(remoteHost(r), r.PostForm.Get("key")); err != nil {
		h.dashboardAuthRejected(r, "bootstrap", fault.OperatorAuthenticationFailed, "recovery_key_rejected")
		http.Error(w, "initial administrator claim rejected", http.StatusForbidden)
		return
	}
	if _, err := h.state.ClaimInitialAdmin(r.Context(), user.ID, requestID(r), "bootstrap:"+user.Subject); err != nil {
		h.dashboardAuthRejected(r, "bootstrap", fault.OperatorAuthorizationDenied, "initial_admin_claim_rejected")
		http.Error(w, "initial administrator claim rejected", http.StatusConflict)
		return
	}
	h.logger.InfoContext(r.Context(), "initial administrator bootstrap accepted", "component", "operator.auth", "operation", "bootstrap", "request_id", requestID(r), "user_id", user.ID, "actor", "bootstrap:"+user.Subject, "result", "success")
	http.Redirect(w, r, "/ui/", http.StatusSeeOther)
}

func (h *DashboardHandler) beginExternalPasskeyLogin(w http.ResponseWriter, r *http.Request) {
	user, ok := h.externalWebAuthnUser(w, r, operatoridentity.StatusActive)
	if !ok {
		return
	}
	options, flow, err := h.passkeys.BeginLogin(user, operatorpasskey.ModeLogin)
	if err != nil {
		h.authJSONFailure(w, r, "passkey_login_start", err)
		return
	}
	writeDashboardJSON(w, http.StatusOK, map[string]any{"options": options, "flow": flow})
}

func (h *DashboardHandler) finishExternalPasskeyLogin(w http.ResponseWriter, r *http.Request) {
	user, ok := h.externalWebAuthnUser(w, r, operatoridentity.StatusActive)
	if !ok {
		return
	}
	credential, err := h.passkeys.FinishLogin(user, strings.TrimSpace(r.Header.Get("X-AegisPXE-Ceremony")), operatorpasskey.ModeLogin, r)
	if err != nil {
		h.authJSONFailure(w, r, "passkey_login_finish", err)
		return
	}
	if err := h.state.SaveOperatorCredential(r.Context(), user.ID, h.passkeys.RPID(), *credential, requestID(r), "user:"+user.Subject, false); err != nil {
		h.authJSONFailure(w, r, "passkey_counter_save", err)
		return
	}
	token, session, err := h.auth.IssueUserSession(user, "zoraxy+passkey")
	if err != nil {
		h.authJSONFailure(w, r, "session_issue", err)
		return
	}
	h.setDashboardSessionCookie(w, r, token, session)
	h.logger.InfoContext(r.Context(), "operator authenticated with outer identity and passkey", "component", "operator.auth", "operation", "login", "request_id", requestID(r), "user_id", user.ID, "actor", session.Actor, "auth_method", session.AuthMethod, "result", "success")
	writeDashboardJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *DashboardHandler) beginPasskeyEnrollment(w http.ResponseWriter, r *http.Request) {
	user, ok := h.externalWebAuthnUser(w, r, operatoridentity.StatusEnrollmentRequired)
	if !ok {
		return
	}
	options, flow, err := h.passkeys.BeginRegistration(user)
	if err != nil {
		h.authJSONFailure(w, r, "passkey_enroll_start", err)
		return
	}
	writeDashboardJSON(w, http.StatusOK, map[string]any{"options": options, "flow": flow})
}

func (h *DashboardHandler) finishPasskeyEnrollment(w http.ResponseWriter, r *http.Request) {
	user, ok := h.externalWebAuthnUser(w, r, operatoridentity.StatusEnrollmentRequired)
	if !ok {
		return
	}
	credential, err := h.passkeys.FinishRegistration(user, strings.TrimSpace(r.Header.Get("X-AegisPXE-Ceremony")), r)
	if err != nil {
		h.authJSONFailure(w, r, "passkey_enroll_finish", err)
		return
	}
	if err := h.state.SaveOperatorCredential(r.Context(), user.ID, h.passkeys.RPID(), *credential, requestID(r), "user:"+user.Subject, true); err != nil {
		h.authJSONFailure(w, r, "passkey_enroll_save", err)
		return
	}
	active, err := h.state.OperatorUser(r.Context(), user.ID)
	if err != nil {
		h.authJSONFailure(w, r, "passkey_enroll_reload", err)
		return
	}
	token, session, err := h.auth.IssueUserSession(active, "zoraxy+passkey")
	if err != nil {
		h.authJSONFailure(w, r, "session_issue", err)
		return
	}
	h.setDashboardSessionCookie(w, r, token, session)
	h.logger.InfoContext(r.Context(), "operator passkey enrollment completed", "component", "operator.auth", "operation", "enroll", "request_id", requestID(r), "user_id", active.ID, "actor", session.Actor, "result", "success")
	writeDashboardJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *DashboardHandler) beginRecoveryPasskey(w http.ResponseWriter, r *http.Request) {
	if _, present := externalIdentityFromRequest(r); present {
		writeDashboardJSON(w, http.StatusForbidden, map[string]string{"message": "Recovery authentication is unavailable while an outer identity is present."})
		return
	}
	if h.passkeys == nil {
		writeDashboardJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "Recovery authentication failed."})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBody)
	if err := r.ParseForm(); err != nil {
		writeDashboardJSON(w, http.StatusBadRequest, map[string]string{"message": "Recovery authentication failed."})
		return
	}
	subject := strings.TrimSpace(r.PostForm.Get("subject"))
	user, err := h.state.OperatorUserByExternalIdentity(r.Context(), trustedProxyProvider, subject)
	if err != nil || user.Status != operatoridentity.StatusActive {
		h.dashboardAuthRejected(r, "recovery_start", fault.OperatorAuthenticationFailed, "unknown_or_inactive_subject")
		writeDashboardJSON(w, http.StatusUnauthorized, map[string]string{"message": "Recovery authentication failed."})
		return
	}
	user, err = h.state.OperatorUserForWebAuthn(r.Context(), user.ID, h.passkeys.RPID())
	if err != nil || len(user.Credentials) == 0 {
		h.dashboardAuthRejected(r, "recovery_start", fault.OperatorAuthenticationFailed, "credential_unavailable")
		writeDashboardJSON(w, http.StatusUnauthorized, map[string]string{"message": "Recovery authentication failed."})
		return
	}
	options, flow, err := h.passkeys.BeginLogin(user, operatorpasskey.ModeRecovery)
	if err != nil {
		h.authJSONFailure(w, r, "recovery_start", err)
		return
	}
	writeDashboardJSON(w, http.StatusOK, map[string]any{"options": options, "flow": flow})
}

func (h *DashboardHandler) finishRecoveryPasskey(w http.ResponseWriter, r *http.Request) {
	if _, present := externalIdentityFromRequest(r); present || h.passkeys == nil {
		writeDashboardJSON(w, http.StatusUnauthorized, map[string]string{"message": "Recovery authentication failed."})
		return
	}
	subject := strings.TrimSpace(r.Header.Get("X-AegisPXE-Recovery-Subject"))
	user, err := h.state.OperatorUserByExternalIdentity(r.Context(), trustedProxyProvider, subject)
	if err != nil || user.Status != operatoridentity.StatusActive {
		writeDashboardJSON(w, http.StatusUnauthorized, map[string]string{"message": "Recovery authentication failed."})
		return
	}
	user, err = h.state.OperatorUserForWebAuthn(r.Context(), user.ID, h.passkeys.RPID())
	if err != nil {
		h.authJSONFailure(w, r, "recovery_passkey", err)
		return
	}
	credential, err := h.passkeys.FinishLogin(user, strings.TrimSpace(r.Header.Get("X-AegisPXE-Ceremony")), operatorpasskey.ModeRecovery, r)
	if err != nil {
		h.authJSONFailure(w, r, "recovery_passkey", err)
		return
	}
	if err := h.state.SaveOperatorCredential(r.Context(), user.ID, h.passkeys.RPID(), *credential, requestID(r), "recovery:"+user.Subject, false); err != nil {
		h.authJSONFailure(w, r, "recovery_counter_save", err)
		return
	}
	ticket, err := operatorpasskey.IssueRecoveryTicket(user.ID)
	if err != nil {
		h.authJSONFailure(w, r, "recovery_ticket", err)
		return
	}
	writeDashboardJSON(w, http.StatusOK, map[string]string{"ticket": ticket})
}

func (h *DashboardHandler) finishRecoveryLogin(w http.ResponseWriter, r *http.Request) {
	if _, present := externalIdentityFromRequest(r); present || !secureTransport(r) {
		http.Error(w, "recovery authentication failed", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBody)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "recovery authentication failed", http.StatusBadRequest)
		return
	}
	subject := strings.TrimSpace(r.PostForm.Get("subject"))
	user, err := h.state.OperatorUserByExternalIdentity(r.Context(), trustedProxyProvider, subject)
	if err != nil || user.Status != operatoridentity.StatusActive {
		http.Error(w, "recovery authentication failed", http.StatusUnauthorized)
		return
	}
	if err := h.auth.VerifyRecoveryKey(remoteHost(r), r.PostForm.Get("key")); err != nil {
		h.dashboardAuthRejected(r, "recovery_finish", fault.OperatorAuthenticationFailed, "recovery_key_rejected")
		http.Error(w, "recovery authentication failed", http.StatusUnauthorized)
		return
	}
	if !operatorpasskey.ConsumeRecoveryTicket(r.PostForm.Get("ticket"), user.ID) {
		h.dashboardAuthRejected(r, "recovery_finish", fault.OperatorAuthenticationFailed, "recovery_ticket_rejected")
		http.Error(w, "recovery authentication failed", http.StatusUnauthorized)
		return
	}
	token, session, err := h.auth.IssueUserSession(user, "recovery+passkey+key")
	if err != nil {
		http.Error(w, "recovery authentication failed", http.StatusUnauthorized)
		return
	}
	h.setDashboardSessionCookie(w, r, token, session)
	h.logger.WarnContext(r.Context(), "emergency operator session created", "component", "operator.auth", "operation", "recovery_login", "request_id", requestID(r), "user_id", user.ID, "actor", session.Actor, "auth_method", session.AuthMethod, "remote", remoteHost(r), "result", "success")
	http.Redirect(w, r, "/ui/", http.StatusSeeOther)
}

func (h *DashboardHandler) dashboardLogout(w http.ResponseWriter, r *http.Request) {
	session, token, ok := h.dashboardSession(r)
	if !ok {
		http.Redirect(w, r, "/ui/", http.StatusSeeOther)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBody)
	if err := r.ParseForm(); err != nil || !h.auth.ValidateCSRF(session, r.PostForm.Get("csrf")) {
		http.Error(w, "invalid dashboard request", http.StatusForbidden)
		return
	}
	h.auth.Logout(token)
	h.clearDashboardSessionCookie(w, r)
	h.logger.InfoContext(r.Context(), "operator session ended", "component", "operator.auth", "operation", "logout", "request_id", requestID(r), "actor", session.Actor, "auth_method", session.AuthMethod, "result", "success")
	http.Redirect(w, r, "/ui/", http.StatusSeeOther)
}

func (h *DashboardHandler) externalWebAuthnUser(w http.ResponseWriter, r *http.Request, required operatoridentity.Status) (operatoridentity.User, bool) {
	identity, ok := externalIdentityFromRequest(r)
	if !ok || h.passkeys == nil {
		writeDashboardJSON(w, http.StatusUnauthorized, map[string]string{"message": "Authentication failed."})
		return operatoridentity.User{}, false
	}
	user, _, err := h.state.ResolveOperatorUser(r.Context(), identity.Provider, identity.Subject, identity.Subject, "", requestID(r))
	if err != nil || user.Status != required {
		h.dashboardAuthRejected(r, "passkey", fault.OperatorAuthenticationFailed, "identity_state_rejected")
		writeDashboardJSON(w, http.StatusUnauthorized, map[string]string{"message": "Authentication failed."})
		return operatoridentity.User{}, false
	}
	user, err = h.state.OperatorUserForWebAuthn(r.Context(), user.ID, h.passkeys.RPID())
	if err != nil {
		h.authJSONFailure(w, r, "passkey_user_load", err)
		return operatoridentity.User{}, false
	}
	if required == operatoridentity.StatusActive && len(user.Credentials) == 0 {
		h.dashboardAuthRejected(r, "passkey", fault.OperatorAuthenticationFailed, "credential_missing")
		writeDashboardJSON(w, http.StatusUnauthorized, map[string]string{"message": "Authentication failed."})
		return operatoridentity.User{}, false
	}
	return user, true
}

func (h *DashboardHandler) authJSONFailure(w http.ResponseWriter, r *http.Request, operation string, err error) {
	h.logger.WarnContext(r.Context(), "operator passkey operation failed", "component", "operator.auth", "operation", operation, "request_id", requestID(r), "remote", remoteHost(r), "error_code", fault.OperatorAuthenticationFailed, "result", "failure", "cause", err.Error())
	writeDashboardJSON(w, http.StatusUnauthorized, map[string]string{"message": "Authentication failed."})
}

func (h *DashboardHandler) renderAuth(w http.ResponseWriter, view authView) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = dashboardAuthTemplate.Execute(w, view)
}

func writeDashboardJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
