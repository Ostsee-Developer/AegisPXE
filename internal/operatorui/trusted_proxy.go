package operatorui

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
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
