package operatorpasskey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/operatoridentity"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const (
	flowTokenBytes = 32
	flowLifetime   = 5 * time.Minute
	maxActiveFlows = 256
)

type Mode string

const (
	ModeLogin      Mode = "login"
	ModeEnrollment Mode = "enrollment"
	ModeRecovery   Mode = "recovery"
)

type flow struct {
	UserID    string
	Mode      Mode
	Session   webauthn.SessionData
	ExpiresAt time.Time
}

type Service struct {
	rpID   string
	wa     *webauthn.WebAuthn
	logger *slog.Logger
	mu     sync.Mutex
	flows  map[[32]byte]flow
	now    func() time.Time
}

func New(rpID string, origins []string, logger *slog.Logger) (*Service, error) {
	rpID = strings.TrimSpace(rpID)
	cleanOrigins := make([]string, 0, len(origins))
	for _, origin := range origins {
		if value := strings.TrimSpace(origin); value != "" {
			cleanOrigins = append(cleanOrigins, value)
		}
	}
	if rpID == "" && len(cleanOrigins) == 0 {
		return nil, nil
	}
	if rpID == "" || len(cleanOrigins) == 0 {
		return nil, errors.New("WebAuthn requires both RP ID and at least one origin")
	}
	if logger == nil {
		logger = slog.Default()
	}
	wa, err := webauthn.New(&webauthn.Config{
		RPDisplayName: "AegisPXE",
		RPID:          rpID,
		RPOrigins:     cleanOrigins,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			UserVerification: protocol.VerificationRequired,
		},
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: flowLifetime, TimeoutUVD: flowLifetime},
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: flowLifetime, TimeoutUVD: flowLifetime},
		},
	})
	if err != nil {
		return nil, err
	}
	return &Service{rpID: rpID, wa: wa, logger: logger, flows: make(map[[32]byte]flow), now: time.Now}, nil
}

func (s *Service) RPID() string {
	if s == nil {
		return ""
	}
	return s.rpID
}

func (s *Service) BeginRegistration(user operatoridentity.User) (*protocol.CredentialCreation, string, error) {
	if s == nil {
		return nil, "", errors.New("WebAuthn is not configured")
	}
	creation, session, err := s.wa.BeginRegistration(user,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementPreferred),
		webauthn.WithExclusions(webauthn.Credentials(user.WebAuthnCredentials()).CredentialDescriptors()),
	)
	if err != nil {
		return nil, "", err
	}
	token, err := s.saveFlow(user.ID, ModeEnrollment, *session)
	if err != nil {
		return nil, "", err
	}
	return creation, token, nil
}

func (s *Service) FinishRegistration(user operatoridentity.User, token string, r *http.Request) (*webauthn.Credential, error) {
	entry, err := s.takeFlow(token, user.ID, ModeEnrollment)
	if err != nil {
		return nil, err
	}
	return s.wa.FinishRegistration(user, entry.Session, r)
}

func (s *Service) BeginLogin(user operatoridentity.User, mode Mode) (*protocol.CredentialAssertion, string, error) {
	if s == nil {
		return nil, "", errors.New("WebAuthn is not configured")
	}
	if mode != ModeLogin && mode != ModeRecovery {
		return nil, "", errors.New("invalid passkey login mode")
	}
	assertion, session, err := s.wa.BeginLogin(user, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, "", err
	}
	token, err := s.saveFlow(user.ID, mode, *session)
	if err != nil {
		return nil, "", err
	}
	return assertion, token, nil
}

func (s *Service) FinishLogin(user operatoridentity.User, token string, mode Mode, r *http.Request) (*webauthn.Credential, error) {
	entry, err := s.takeFlow(token, user.ID, mode)
	if err != nil {
		return nil, err
	}
	return s.wa.FinishLogin(user, entry.Session, r)
}

func (s *Service) saveFlow(userID string, mode Mode, session webauthn.SessionData) (string, error) {
	token, err := randomToken(flowTokenBytes)
	if err != nil {
		return "", err
	}
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
	if len(s.flows) >= maxActiveFlows {
		return "", errors.New("too many active passkey ceremonies")
	}
	s.flows[sha256.Sum256([]byte(token))] = flow{UserID: userID, Mode: mode, Session: session, ExpiresAt: now.Add(flowLifetime)}
	return token, nil
}

func (s *Service) takeFlow(token, userID string, mode Mode) (flow, error) {
	if s == nil || strings.TrimSpace(token) == "" {
		return flow{}, errors.New("passkey ceremony is missing")
	}
	now := s.now().UTC()
	digest := sha256.Sum256([]byte(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
	entry, ok := s.flows[digest]
	delete(s.flows, digest)
	if !ok || !entry.ExpiresAt.After(now) || entry.UserID != userID || entry.Mode != mode {
		return flow{}, errors.New("passkey ceremony is invalid or expired")
	}
	return entry, nil
}

func (s *Service) cleanupLocked(now time.Time) {
	for digest, entry := range s.flows {
		if !entry.ExpiresAt.After(now) {
			delete(s.flows, digest)
		}
	}
}

func randomToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
