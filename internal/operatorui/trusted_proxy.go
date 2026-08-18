package operatorui

import (
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
	"github.com/Ostsee-Developer/AegisPXE/internal/operator"
	"github.com/Ostsee-Developer/AegisPXE/internal/store"
)

type TrustedProxy struct {
	prefixes       []netip.Prefix
	identityHeader string
	protoHeader    string
}

func ParseTrustedProxy(cidrs, identityHeader, protoHeader string) (TrustedProxy, error) {
	cidrs = strings.TrimSpace(cidrs)
	if cidrs == "" {
		return TrustedProxy{}, nil
	}
	identityHeader = strings.TrimSpace(identityHeader)
	protoHeader = strings.TrimSpace(protoHeader)
	if !validHeaderName(identityHeader) || !validHeaderName(protoHeader) {
		return TrustedProxy{}, errors.New("trusted proxy header names are invalid")
	}

	parts := strings.FieldsFunc(cidrs, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n'
	})
	prefixes := make([]netip.Prefix, 0, len(parts))
	for _, value := range parts {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(value); err == nil {
			prefixes = append(prefixes, prefix.Masked())
			continue
		}
		addr, err := netip.ParseAddr(value)
		if err != nil {
			return TrustedProxy{}, errors.New("trusted proxy CIDR contains an invalid address")
		}
		bits := 128
		if addr.Is4() {
			bits = 32
		}
		prefixes = append(prefixes, netip.PrefixFrom(addr, bits))
	}
	if len(prefixes) == 0 {
		return TrustedProxy{}, errors.New("trusted proxy CIDRs are empty")
	}
	return TrustedProxy{prefixes: prefixes, identityHeader: identityHeader, protoHeader: protoHeader}, nil
}

func (p TrustedProxy) Enabled() bool {
	return len(p.prefixes) > 0
}

func (p TrustedProxy) Identity(r *http.Request) (string, bool) {
	if !p.Enabled() || r == nil {
		return "", false
	}
	addr, ok := remoteAddr(r.RemoteAddr)
	if !ok || !p.contains(addr) {
		return "", false
	}
	if strings.ToLower(strings.TrimSpace(r.Header.Get(p.protoHeader))) != "https" {
		return "", false
	}
	identity := strings.TrimSpace(r.Header.Get(p.identityHeader))
	if identity == "" || len(identity) > 120 {
		return "", false
	}
	for _, ch := range identity {
		if ch < 0x20 || ch == 0x7f {
			return "", false
		}
	}
	return identity, true
}

func NewWithTrustedProxy(next http.Handler, state *store.Store, auth *operator.Manager, logger *slog.Logger, proxy TrustedProxy) http.Handler {
	base := New(next, state, auth, logger)
	if !proxy.Enabled() {
		return base
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &trustedProxySessionHandler{next: base, auth: auth, logger: logger, proxy: proxy}
}

type trustedProxySessionHandler struct {
	next   http.Handler
	auth   *operator.Manager
	logger *slog.Logger
	proxy  TrustedProxy
}

func (h *trustedProxySessionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	identity, ok := h.proxy.Identity(r)
	if !ok {
		h.next.ServeHTTP(w, r)
		return
	}

	request := r.Clone(r.Context())
	// AegisPXE only sets this after the direct peer and forwarded protocol
	// have passed the TrustedProxy contract above. Downstream code can then
	// treat the request as HTTPS-terminated without trusting arbitrary headers.
	request.TLS = &tls.ConnectionState{}
	if requestID(request) == "" {
		id, err := idgen.New("req_")
		if err != nil {
			http.Error(w, "operator service unavailable", http.StatusServiceUnavailable)
			return
		}
		request.Header.Set("X-Request-ID", id)
	}

	if h.auth != nil {
		cookie, cookieErr := request.Cookie(sessionCookieName)
		if cookieErr != nil || !validSessionCookie(h.auth, cookie) {
			token, session, err := h.auth.IssueSession("proxy:" + identity)
			if err != nil {
				h.logger.ErrorContext(request.Context(), "trusted proxy session failed",
					"component", "operator.auth",
					"operation", "proxy_session",
					"request_id", requestID(request),
					"remote", remoteHost(request),
					"error_code", fault.StorageFailure,
					"result", "failure",
					"cause", err.Error(),
				)
				http.Error(w, "operator service unavailable", http.StatusServiceUnavailable)
				return
			}
			cookie = &http.Cookie{
				Name:     sessionCookieName,
				Value:    token,
				Path:     "/ui/",
				HttpOnly: true,
				Secure:   true,
				SameSite: http.SameSiteStrictMode,
				Expires:  session.ExpiresAt,
				MaxAge:   int(operator.SessionDuration.Seconds()),
			}
			http.SetCookie(w, cookie)
			request.AddCookie(cookie)
			h.logger.InfoContext(request.Context(), "operator authenticated by trusted proxy",
				"component", "operator.auth",
				"operation", "proxy_session",
				"request_id", requestID(request),
				"remote", remoteHost(request),
				"actor", session.Actor,
				"result", "success",
			)
		}
	}
	h.next.ServeHTTP(w, request)
}

func RequireTrustedProxyOrLoopback(next http.Handler, proxy TrustedProxy, logger *slog.Logger) http.Handler {
	if !proxy.Enabled() {
		return next
	}
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if addr, ok := remoteAddr(r.RemoteAddr); ok && addr.IsLoopback() {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := proxy.Identity(r); ok {
			next.ServeHTTP(w, r)
			return
		}
		logger.WarnContext(r.Context(), "studio request rejected outside trusted proxy boundary",
			"component", "operator.proxy",
			"operation", "gate",
			"remote", remoteHost(r),
			"error_code", fault.OperatorSecureTransportRequired,
			"result", "rejected",
			"cause", "untrusted_proxy_source_or_identity",
		)
		http.Error(w, "studio access denied", http.StatusForbidden)
	})
}

func validSessionCookie(auth *operator.Manager, cookie *http.Cookie) bool {
	if auth == nil || cookie == nil || cookie.Value == "" {
		return false
	}
	_, ok := auth.Session(cookie.Value)
	return ok
}

func (p TrustedProxy) contains(addr netip.Addr) bool {
	for _, prefix := range p.prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func remoteAddr(value string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(value))
	if err != nil {
		host = strings.TrimSpace(value)
	}
	addr, err := netip.ParseAddr(host)
	return addr, err == nil
}

func validHeaderName(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		switch {
		case ch >= 'a' && ch <= 'z':
		case ch >= 'A' && ch <= 'Z':
		case ch >= '0' && ch <= '9':
		case strings.ContainsRune("!#$%&'*+-.^_`|~", ch):
		default:
			return false
		}
	}
	return true
}
