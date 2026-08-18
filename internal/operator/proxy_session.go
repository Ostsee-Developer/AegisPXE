package operator

import (
	"errors"
	"fmt"
	"strings"
)

// IssueSession creates a normal server-side operator session for an identity
// that has already been authenticated by an explicitly trusted boundary.
// The caller owns that authentication decision; this method never accepts
// headers, network identities, or other external trust signals directly.
func (m *Manager) IssueSession(actor string) (string, Session, error) {
	if m == nil {
		return "", Session{}, errors.New("operator manager is unavailable")
	}
	actor = strings.TrimSpace(actor)
	if actor == "" || len(actor) > 128 {
		return "", Session{}, errors.New("operator actor is invalid")
	}
	for _, r := range actor {
		if r < 0x20 || r == 0x7f {
			return "", Session{}, errors.New("operator actor contains control characters")
		}
	}

	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(now)

	token, err := randomToken(sessionTokenBytes)
	if err != nil {
		return "", Session{}, fmt.Errorf("generate operator session: %w", err)
	}
	csrf, err := randomToken(csrfTokenBytes)
	if err != nil {
		return "", Session{}, fmt.Errorf("generate operator csrf token: %w", err)
	}
	session := Session{
		Actor:     actor,
		CSRFToken: csrf,
		ExpiresAt: now.Add(SessionDuration),
	}
	m.sessions[sha256Digest(token)] = sessionRecord{Session: session}
	return token, session, nil
}

func sha256Digest(value string) [32]byte {
	return sha256Sum([]byte(value))
}
