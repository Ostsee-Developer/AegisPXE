package operatoridentity

import (
	"errors"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

type Role string

type Status string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"

	StatusPendingReview      Status = "pending_review"
	StatusEnrollmentRequired Status = "enrollment_required"
	StatusActive             Status = "active"
	StatusBlocked            Status = "blocked"
)

type User struct {
	ID             string
	Provider       string
	Subject        string
	DisplayName    string
	Email          string
	Role           Role
	Status         Status
	WebAuthnHandle []byte
	Credentials    []webauthn.Credential
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ApprovedAt     time.Time
	ApprovedBy     string
}

func (u User) WebAuthnID() []byte {
	out := make([]byte, len(u.WebAuthnHandle))
	copy(out, u.WebAuthnHandle)
	return out
}

func (u User) WebAuthnName() string {
	if value := strings.TrimSpace(u.DisplayName); value != "" {
		return value
	}
	return u.Subject
}

func (u User) WebAuthnDisplayName() string {
	return u.WebAuthnName()
}

func (u User) WebAuthnCredentials() []webauthn.Credential {
	out := make([]webauthn.Credential, len(u.Credentials))
	copy(out, u.Credentials)
	return out
}

func ValidateRole(role Role) error {
	switch role {
	case RoleAdmin, RoleOperator:
		return nil
	default:
		return errors.New("operator role is invalid")
	}
}

func ValidateStatus(status Status) error {
	switch status {
	case StatusPendingReview, StatusEnrollmentRequired, StatusActive, StatusBlocked:
		return nil
	default:
		return errors.New("operator user status is invalid")
	}
}

func ValidateExternalIdentity(provider, subject string) error {
	provider = strings.TrimSpace(provider)
	subject = strings.TrimSpace(subject)
	if provider == "" || len(provider) > 64 {
		return errors.New("operator identity provider is invalid")
	}
	if subject == "" || len(subject) > 128 {
		return errors.New("operator identity subject is invalid")
	}
	for _, value := range []string{provider, subject} {
		for _, r := range value {
			if r < 0x20 || r == 0x7f {
				return errors.New("operator identity contains control characters")
			}
		}
	}
	return nil
}
