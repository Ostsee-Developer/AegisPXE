package secureboot

import (
	"errors"
	"strings"
)

type State string

type Policy string

const (
	StateUnknown     State = "unknown"
	StateEnabled     State = "enabled"
	StateDisabled    State = "disabled"
	StateSetupMode   State = "setup_mode"
	StateUnsupported State = "unsupported"

	PolicyRequired Policy = "required"
	PolicyAudit    Policy = "audit"
	PolicyDisabled Policy = "disabled"
)

func ParsePolicy(value string) (Policy, error) {
	policy := Policy(strings.ToLower(strings.TrimSpace(value)))
	switch policy {
	case PolicyRequired, PolicyAudit, PolicyDisabled:
		return policy, nil
	default:
		return "", errors.New("secure boot policy must be required, audit, or disabled")
	}
}

func Observe(firmware, secureBootHex, setupModeHex string) (State, error) {
	firmware = strings.ToLower(strings.TrimSpace(firmware))
	secureBootHex = normalizeByte(secureBootHex)
	setupModeHex = normalizeByte(setupModeHex)

	if firmware != "efi" {
		return StateUnsupported, nil
	}
	if secureBootHex == "" && setupModeHex == "" {
		return StateUnknown, nil
	}
	if !validFlag(secureBootHex) || !validFlag(setupModeHex) {
		return StateUnknown, errors.New("invalid UEFI SecureBoot or SetupMode value")
	}
	if setupModeHex == "01" {
		return StateSetupMode, nil
	}
	if secureBootHex == "01" {
		return StateEnabled, nil
	}
	return StateDisabled, nil
}

func (p Policy) AllowsProvision(state State) bool {
	switch p {
	case PolicyDisabled, PolicyAudit:
		return true
	case PolicyRequired:
		return state == StateEnabled
	default:
		return false
	}
}

func (p Policy) RequiresSecureBoot() bool {
	return p == PolicyRequired
}

func normalizeByte(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "0x")
	switch value {
	case "0":
		return "00"
	case "1":
		return "01"
	default:
		return value
	}
}

func validFlag(value string) bool {
	return value == "00" || value == "01"
}
