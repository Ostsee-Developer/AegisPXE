package agent

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	MaxHeartbeatBootIDRunes   = 128
	MaxHeartbeatHostnameRunes = 253
	MaxHeartbeatKernelRunes   = 256
	OnlineThreshold           = 90 * time.Second
	DegradedThreshold         = 180 * time.Second
)

type EnrollmentCredential struct {
	ID         string
	AgentID    string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	ConsumedAt time.Time
	RevokedAt  time.Time
}

type Certificate struct {
	Fingerprint     string
	AgentID         string
	Serial          string
	PublicKeySHA256 string
	IssuedAt        time.Time
	ExpiresAt       time.Time
	RevokedAt       time.Time
	RevokedBy       string
}

type Heartbeat struct {
	Version       string `json:"version"`
	Generation    int    `json:"generation"`
	BootID        string `json:"boot_id"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	Hostname      string `json:"hostname"`
	Kernel        string `json:"kernel"`
	Architecture  string `json:"architecture"`
}

func (h Heartbeat) Normalize() (Heartbeat, error) {
	h.Version = strings.TrimSpace(h.Version)
	h.BootID = strings.TrimSpace(h.BootID)
	h.Hostname = strings.TrimSpace(h.Hostname)
	h.Kernel = strings.TrimSpace(h.Kernel)
	h.Architecture = strings.TrimSpace(h.Architecture)
	if h.Version == "" || len(h.Version) > 128 || strings.ContainsAny(h.Version, "\r\n\x00") {
		return Heartbeat{}, errors.New("agent heartbeat version is invalid")
	}
	if h.Generation < 1 {
		return Heartbeat{}, errors.New("agent heartbeat generation is invalid")
	}
	if h.UptimeSeconds < 0 {
		return Heartbeat{}, errors.New("agent heartbeat uptime is invalid")
	}
	if err := validateBoundedText("boot ID", h.BootID, MaxHeartbeatBootIDRunes, false); err != nil {
		return Heartbeat{}, err
	}
	if err := validateBoundedText("hostname", h.Hostname, MaxHeartbeatHostnameRunes, true); err != nil {
		return Heartbeat{}, err
	}
	if err := validateBoundedText("kernel", h.Kernel, MaxHeartbeatKernelRunes, true); err != nil {
		return Heartbeat{}, err
	}
	if h.Architecture != "amd64" && h.Architecture != "arm64" {
		return Heartbeat{}, fmt.Errorf("unsupported agent heartbeat architecture %q", h.Architecture)
	}
	return h, nil
}

func ProjectPresence(record Record, now time.Time) State {
	if record.State == StateRevoked || record.State == StatePendingBuild || record.State == StateReady || record.State == StateUnenrolled || record.State == StateEnrolling {
		return record.State
	}
	if record.LastSeenAt.IsZero() {
		return StateOffline
	}
	age := now.UTC().Sub(record.LastSeenAt.UTC())
	if age < 0 {
		age = 0
	}
	switch {
	case age < OnlineThreshold:
		return StateOnline
	case age < DegradedThreshold:
		return StateDegraded
	default:
		return StateOffline
	}
}

func validateBoundedText(name, value string, maxRunes int, allowEmpty bool) error {
	if !allowEmpty && value == "" {
		return fmt.Errorf("agent heartbeat %s is required", name)
	}
	if strings.ContainsAny(value, "\r\n\x00") || len([]rune(value)) > maxRunes {
		return fmt.Errorf("agent heartbeat %s is invalid", name)
	}
	return nil
}
