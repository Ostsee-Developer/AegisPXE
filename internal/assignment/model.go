package assignment

import (
	"errors"
	"strings"
	"time"
)

type State string

const (
	StateArmed     State = "armed"
	StateConsumed  State = "consumed"
	StateCancelled State = "cancelled"
)

const TrustRequirementCryptographic = "cryptographic"

type Assignment struct {
	ID               string
	MachineID        string
	InstallationID   string
	State            State
	TrustRequirement string
	ArmedAt          time.Time
	ArmedBy          string
	ConsumedAt       time.Time
	CancelledAt      time.Time
}

func (a Assignment) Validate() error {
	if strings.TrimSpace(a.MachineID) == "" || strings.TrimSpace(a.InstallationID) == "" {
		return errors.New("assignment machine and installation IDs are required")
	}
	if a.TrustRequirement != TrustRequirementCryptographic {
		return errors.New("assignment requires unsupported boot trust")
	}
	switch a.State {
	case StateArmed:
		if a.ArmedAt.IsZero() || strings.TrimSpace(a.ArmedBy) == "" || !a.ConsumedAt.IsZero() || !a.CancelledAt.IsZero() {
			return errors.New("armed assignment metadata is invalid")
		}
	case StateConsumed:
		if a.ArmedAt.IsZero() || a.ConsumedAt.IsZero() || !a.CancelledAt.IsZero() {
			return errors.New("consumed assignment metadata is invalid")
		}
	case StateCancelled:
		if a.ArmedAt.IsZero() || a.CancelledAt.IsZero() || !a.ConsumedAt.IsZero() {
			return errors.New("cancelled assignment metadata is invalid")
		}
	default:
		return errors.New("assignment state is invalid")
	}
	return nil
}
