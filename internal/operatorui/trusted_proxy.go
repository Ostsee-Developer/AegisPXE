package operatorui

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
)

const trustedProxyProvider = "trusted-proxy"

type externalIdentity struct {
	Provider string
	Subject  string
}

type externalIdentityContextKey struct{}

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

func (p TrustedProxy) SecureSource(r *http.Request) bool {
	if !p.Enabled() || r == nil {
		return false
	}
	addr, ok := remoteAddr(r.RemoteAddr)
	if !ok || !p.contains(addr) {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get(p.protoHeader)), "https")
}

func (p TrustedProxy) Identity(r *http.Request) (string, bool) {
	if !p.SecureSource(r) {
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

type trustedProxyIdentityHandler struct {
	next   http.Handler
	logger *slog.Logger
	proxy  TrustedProxy
}

func (h *trustedProxyIdentityHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.proxy.SecureSource(r) {
		h.next.ServeHTTP(w, r)
		return
	}

	request := r.Clone(r.Context())
	request.TLS = &tls.ConnectionState{}
	if requestID(request) == "" {
		id, err := idgen.New("req_")
		if err != nil {
			http.Error(w, "operator service unavailable", http.StatusServiceUnavailable)
			return
		}
		request.Header.Set("X-Request-ID", id)
	}
	if subject, ok := h.proxy.Identity(r); ok {
		identity := externalIdentity{Provider: trustedProxyProvider, Subject: subject}
		request = request.WithContext(context.WithValue(request.Context(), externalIdentityContextKey{}, identity))
		// Successful proxy identity injection happens for every Studio request,
		// including assets and polling. Keep it available for deep diagnostics
		// without flooding normal INFO logs; actual passkey authentication and
		// security-relevant rejections remain INFO/WARN audit signals.
		h.logger.DebugContext(request.Context(), "trusted proxy identity accepted",
			"component", "operator.proxy",
			"operation", "identity",
			"request_id", requestID(request),
			"remote", remoteHost(request),
			"method", request.Method,
			"path", request.URL.Path,
			"provider", identity.Provider,
			"external_subject", identity.Subject,
			"result", "accepted",
		)
	}
	h.next.ServeHTTP(w, request)
}

func externalIdentityFromRequest(r *http.Request) (externalIdentity, bool) {
	if r == nil {
		return externalIdentity{}, false
	}
	identity, ok := r.Context().Value(externalIdentityContextKey{}).(externalIdentity)
	return identity, ok && strings.TrimSpace(identity.Subject) != ""
}

func RequireTrustedProxyOrLoopback(next http.Handler, proxy TrustedProxy, logger *slog.Logger) http.Handler {
	if !proxy.Enabled() {
		return next
	}
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /healthz is deliberately non-sensitive and is already exposed by the
		// PXE listener. Allow GET/HEAD probes on Studio without requiring the
		// reverse proxy to synthesize browser-facing HTTPS headers.
		if r != nil && r.URL != nil && r.URL.Path == "/healthz" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			next.ServeHTTP(w, r)
			return
		}
		if addr, ok := remoteAddr(r.RemoteAddr); ok && addr.IsLoopback() {
			next.ServeHTTP(w, r)
			return
		}
		if proxy.SecureSource(r) {
			next.ServeHTTP(w, r)
			return
		}
		path := ""
		method := ""
		if r != nil {
			method = r.Method
			if r.URL != nil {
				path = r.URL.Path
			}
		}
		logger.WarnContext(r.Context(), "dashboard request rejected outside trusted proxy boundary",
			"component", "operator.proxy",
			"operation", "gate",
			"remote", remoteHost(r),
			"method", method,
			"path", path,
			"error_code", fault.OperatorSecureTransportRequired,
			"result", "rejected",
			"cause", "untrusted_proxy_source_or_protocol",
		)
		http.Error(w, "dashboard access denied", http.StatusForbidden)
	})
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
