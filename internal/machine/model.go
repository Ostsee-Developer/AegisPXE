package machine

import (
	"errors"
	"net"
	"regexp"
	"strings"
	"time"
	"unicode"
)

type Policy string

const (
	PolicyPending   Policy = "pending"
	PolicyLocal     Policy = "local"
	PolicyProvision Policy = "provision"
	PolicyBlocked   Policy = "blocked"
	MaxNicknameRunes       = 80
)

type IdentifierKind string

const (
	IdentifierMAC        IdentifierKind = "mac"
	IdentifierSMBIOSUUID IdentifierKind = "smbios_uuid"
)

type Identifier struct {
	Kind  IdentifierKind
	Value string
}

type Observation struct {
	MAC          string
	SMBIOSUUID   string
	Architecture string
	Firmware     string
}

type Machine struct {
	ID           string
	Nickname     string
	Policy       Policy
	Architecture string
	Firmware     string
	FirstSeen    time.Time
	LastSeen     time.Time
}

var smbiosUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func NormalizeNickname(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len([]rune(value)) > MaxNicknameRunes {
		return "", errors.New("machine nickname is too long")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", errors.New("machine nickname contains control characters")
		}
	}
	return value, nil
}

func (o Observation) Identifiers() ([]Identifier, error) {
	var out []Identifier
	if strings.TrimSpace(o.MAC) != "" {
		hw, err := net.ParseMAC(o.MAC)
		if err != nil || len(hw) != 6 {
			return nil, errors.New("invalid MAC address")
		}
		out = append(out, Identifier{Kind: IdentifierMAC, Value: strings.ToLower(hw.String())})
	}
	if strings.TrimSpace(o.SMBIOSUUID) != "" {
		value := strings.ToLower(strings.TrimSpace(o.SMBIOSUUID))
		if !smbiosUUID.MatchString(value) {
			return nil, errors.New("invalid SMBIOS UUID")
		}
		out = append(out, Identifier{Kind: IdentifierSMBIOSUUID, Value: value})
	}
	if len(out) == 0 {
		return nil, errors.New("at least one machine identifier is required")
	}
	return out, nil
}
