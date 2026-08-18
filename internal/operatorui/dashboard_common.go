package operatorui

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"unicode"

	"github.com/Ostsee-Developer/AegisPXE/internal/drivers/debian13"
)

const (
	sessionCookieName       = "aegispxe_operator_session"
	maxFormBody             = 96 << 10
	builtinProfileID        = "builtin:debian13-standard"
	builtinProfileRevision  = "1"
)

type debianArtifactResolver interface {
	Resolve(context.Context) (debian13.Resolution, error)
}

type wizardValues struct {
	MachineID     string
	Hostname      string
	Locale        string
	Keyboard      string
	Timezone      string
	AdminUsername string
	AdminFullName string
	SSHKeys       string
	Packages      string
	TargetDisk    string
}

func newDebianArtifactResolver(logger *slog.Logger) debianArtifactResolver {
	return debian13.NewArtifactResolver(logger)
}

func defaultWizardValues() wizardValues {
	return wizardValues{
		Hostname:      "debian13",
		Locale:        "de_DE.UTF-8",
		Keyboard:      "de",
		Timezone:      "Europe/Berlin",
		AdminUsername: "guardian",
		AdminFullName: "Aegis Administrator",
		TargetDisk:    "/dev/vda",
	}
}

func wizardValuesFromRequest(r *http.Request) wizardValues {
	return wizardValues{
		MachineID:     strings.TrimSpace(r.PostForm.Get("machine_id")),
		Hostname:      r.PostForm.Get("hostname"),
		Locale:        r.PostForm.Get("locale"),
		Keyboard:      r.PostForm.Get("keyboard"),
		Timezone:      r.PostForm.Get("timezone"),
		AdminUsername: r.PostForm.Get("admin_username"),
		AdminFullName: r.PostForm.Get("admin_full_name"),
		SSHKeys:       r.PostForm.Get("ssh_keys"),
		Packages:      r.PostForm.Get("packages"),
		TargetDisk:    r.PostForm.Get("target_disk"),
	}
}

func parseSSHKeys(value string) []string {
	var keys []string
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			keys = append(keys, line)
		}
	}
	return keys
}

func parsePackages(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == ';'
	})
	seen := make(map[string]struct{}, len(fields))
	packages := make([]string, 0, len(fields))
	for _, field := range fields {
		name := strings.TrimSpace(field)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		packages = append(packages, name)
	}
	sort.Strings(packages)
	return packages
}

func secureTransport(r *http.Request) bool {
	if r != nil && r.TLS != nil {
		return true
	}
	if r == nil {
		return false
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func remoteHost(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
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
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.Header.Get("X-Request-ID"))
}
