package profile

import (
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
)

const SchemaVersion = 1

var (
	hostnamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	usernamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	localePattern   = regexp.MustCompile(`^[A-Za-z0-9_.@-]{1,64}$`)
	keymapPattern   = regexp.MustCompile(`^[A-Za-z0-9_.+-]{1,64}$`)
	timezonePattern = regexp.MustCompile(`^[A-Za-z0-9_+.-]+(?:/[A-Za-z0-9_+.-]+)*$`)
	packagePattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]{0,127}$`)
)

type Snapshot struct {
	SchemaVersion int      `json:"schema_version"`
	Hostname      string   `json:"hostname"`
	Locale        string   `json:"locale"`
	Keyboard      string   `json:"keyboard"`
	Timezone      string   `json:"timezone"`
	Admin         Admin    `json:"admin"`
	Packages      []string `json:"packages,omitempty"`
}

type Admin struct {
	Username          string   `json:"username"`
	FullName          string   `json:"full_name"`
	AuthorizedSSHKeys []string `json:"authorized_ssh_keys"`
	PasswordlessSudo  bool     `json:"passwordless_sudo"`
}

func (s Snapshot) Validate() error {
	if s.SchemaVersion != SchemaVersion {
		return errors.New("unsupported profile snapshot schema version")
	}
	if !hostnamePattern.MatchString(s.Hostname) {
		return errors.New("profile hostname is invalid")
	}
	if !localePattern.MatchString(s.Locale) {
		return errors.New("profile locale is invalid")
	}
	if !keymapPattern.MatchString(s.Keyboard) {
		return errors.New("profile keyboard layout is invalid")
	}
	if len(s.Timezone) > 128 || !timezonePattern.MatchString(s.Timezone) || strings.Contains(s.Timezone, "..") {
		return errors.New("profile timezone is invalid")
	}
	if !usernamePattern.MatchString(s.Admin.Username) {
		return errors.New("profile admin username is invalid")
	}
	if name := strings.TrimSpace(s.Admin.FullName); name == "" || len(name) > 128 || strings.ContainsAny(name, "\r\n") {
		return errors.New("profile admin full name is invalid")
	}
	if len(s.Admin.AuthorizedSSHKeys) == 0 || len(s.Admin.AuthorizedSSHKeys) > 8 {
		return errors.New("profile requires between one and eight SSH public keys")
	}
	for _, key := range s.Admin.AuthorizedSSHKeys {
		if err := validateAuthorizedKey(key); err != nil {
			return err
		}
	}
	if len(s.Packages) > 64 {
		return errors.New("profile requests too many packages")
	}
	seen := make(map[string]struct{}, len(s.Packages))
	for _, name := range s.Packages {
		if !packagePattern.MatchString(name) {
			return errors.New("profile package name is invalid")
		}
		if _, exists := seen[name]; exists {
			return errors.New("profile contains duplicate package names")
		}
		seen[name] = struct{}{}
	}
	return nil
}

func (s Snapshot) Clone() Snapshot {
	copy := s
	copy.Admin.AuthorizedSSHKeys = append([]string(nil), s.Admin.AuthorizedSSHKeys...)
	copy.Packages = append([]string(nil), s.Packages...)
	return copy
}

func validateAuthorizedKey(value string) error {
	if len(value) == 0 || len(value) > 8192 || strings.ContainsAny(value, "\r\n") {
		return errors.New("profile SSH public key is invalid")
	}
	fields := strings.Fields(value)
	if len(fields) < 2 {
		return errors.New("profile SSH public key is invalid")
	}
	switch fields[0] {
	case "ssh-ed25519", "ssh-rsa", "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521", "sk-ssh-ed25519@openssh.com", "sk-ecdsa-sha2-nistp256@openssh.com":
	default:
		return errors.New("profile SSH public key type is unsupported")
	}
	raw, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil || len(raw) < 32 || len(raw) > 16384 {
		return errors.New("profile SSH public key payload is invalid")
	}
	return nil
}
