package operator

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/operatoridentity"
)

const (
	recoveryKeyBytes  = 32
	sessionTokenBytes = 32
	csrfTokenBytes    = 24
	SessionDuration   = 8 * time.Hour
	loginWindow       = time.Minute
	maxLoginAttempts  = 5
	maxActiveSessions = 1024
)

type Session struct {
	Actor      string
	UserID     string
	Role       operatoridentity.Role
	AuthMethod string
	CSRFToken  string
	ExpiresAt  time.Time
}

func (s Session) IsAdmin() bool {
	return s.Role == operatoridentity.RoleAdmin
}

type sessionRecord struct {
	Session
}

type loginCounter struct {
	Started time.Time
	Count   int
}

type Manager struct {
	keyDigest [32]byte
	logger    *slog.Logger
	mu        sync.Mutex
	sessions  map[[32]byte]sessionRecord
	attempts  map[string]loginCounter
	now       func() time.Time
}

func LoadOrCreate(path string, logger *slog.Logger) (*Manager, error) {
	if logger == nil {
		logger = slog.Default()
	}
	path = filepath.Clean(strings.TrimSpace(path))
	if !filepath.IsAbs(path) {
		return nil, errors.New("operator key path must be absolute")
	}
	key, created, err := loadOrCreateRecoveryKey(path)
	if err != nil {
		return nil, err
	}
	manager, err := New(key, logger)
	if err != nil {
		return nil, err
	}
	if created {
		logger.Info("operator recovery key created",
			"component", "operator.auth",
			"operation", "recovery_key",
			"path", path,
			"result", "created",
		)
	} else {
		logger.Info("operator recovery key loaded",
			"component", "operator.auth",
			"operation", "recovery_key",
			"path", path,
			"result", "loaded",
		)
	}
	return manager, nil
}

func New(key string, logger *slog.Logger) (*Manager, error) {
	key = strings.TrimSpace(key)
	decoded, err := base64.RawURLEncoding.DecodeString(key)
	if err != nil || len(decoded) != recoveryKeyBytes {
		return nil, errors.New("operator recovery key must be 256-bit base64url without padding")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		keyDigest: sha256.Sum256([]byte(key)),
		logger:    logger,
		sessions:  make(map[[32]byte]sessionRecord),
		attempts:  make(map[string]loginCounter),
		now:       time.Now,
	}, nil
}

func GenerateKey() (string, error) {
	return randomToken(recoveryKeyBytes)
}

func (m *Manager) IssueUserSession(user operatoridentity.User, authMethod string) (string, Session, error) {
	if m == nil {
		return "", Session{}, errors.New("operator manager is unavailable")
	}
	if user.ID == "" || user.Status != operatoridentity.StatusActive {
		return "", Session{}, errors.New("operator user is not active")
	}
	if err := operatoridentity.ValidateRole(user.Role); err != nil {
		return "", Session{}, err
	}
	authMethod = strings.TrimSpace(authMethod)
	if authMethod == "" || len(authMethod) > 64 {
		return "", Session{}, errors.New("operator authentication method is invalid")
	}
	return m.issueSession(Session{
		Actor:      "user:" + user.Subject,
		UserID:     user.ID,
		Role:       user.Role,
		AuthMethod: authMethod,
	})
}

func (m *Manager) issueSession(base Session) (string, Session, error) {
	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(now)
	if len(m.sessions) >= maxActiveSessions {
		return "", Session{}, errors.New("too many active operator sessions")
	}

	token, err := randomToken(sessionTokenBytes)
	if err != nil {
		return "", Session{}, fmt.Errorf("generate operator session: %w", err)
	}
	csrf, err := randomToken(csrfTokenBytes)
	if err != nil {
		return "", Session{}, fmt.Errorf("generate operator csrf token: %w", err)
	}
	base.CSRFToken = csrf
	base.ExpiresAt = now.Add(SessionDuration)
	m.sessions[sha256.Sum256([]byte(token))] = sessionRecord{Session: base}
	return token, base, nil
}

func (m *Manager) Session(token string) (Session, bool) {
	if m == nil || strings.TrimSpace(token) == "" {
		return Session{}, false
	}
	now := m.now().UTC()
	digest := sha256.Sum256([]byte(token))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(now)
	record, ok := m.sessions[digest]
	if !ok || !record.ExpiresAt.After(now) {
		return Session{}, false
	}
	return record.Session, true
}

func (m *Manager) ValidateCSRF(session Session, supplied string) bool {
	if strings.TrimSpace(supplied) == "" || session.CSRFToken == "" {
		return false
	}
	left := sha256.Sum256([]byte(supplied))
	right := sha256.Sum256([]byte(session.CSRFToken))
	return subtle.ConstantTimeCompare(left[:], right[:]) == 1
}

func (m *Manager) Logout(token string) {
	if m == nil || token == "" {
		return
	}
	digest := sha256.Sum256([]byte(token))
	m.mu.Lock()
	delete(m.sessions, digest)
	m.mu.Unlock()
}

func (m *Manager) cleanupLocked(now time.Time) {
	for digest, record := range m.sessions {
		if !record.ExpiresAt.After(now) {
			delete(m.sessions, digest)
		}
	}
	for remote, counter := range m.attempts {
		if now.Sub(counter.Started) >= 2*loginWindow {
			delete(m.attempts, remote)
		}
	}
}

func loadOrCreateRecoveryKey(path string) (string, bool, error) {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", false, errors.New("operator key path must be a regular file")
		}
		if info.Mode().Perm()&0o077 != 0 {
			return "", false, errors.New("operator key file permissions must not allow group/other access")
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return "", false, fmt.Errorf("read operator recovery key: %w", err)
		}
		return strings.TrimSpace(string(content)), false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", false, fmt.Errorf("inspect operator recovery key: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", false, fmt.Errorf("create operator key directory: %w", err)
	}
	key, err := GenerateKey()
	if err != nil {
		return "", false, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", false, fmt.Errorf("create operator recovery key: %w", err)
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.WriteString(key + "\n"); err != nil {
		return "", false, fmt.Errorf("write operator recovery key: %w", err)
	}
	if err := file.Sync(); err != nil {
		return "", false, fmt.Errorf("sync operator recovery key: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", false, fmt.Errorf("close operator recovery key: %w", err)
	}
	ok = true
	return key, true, nil
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("secure random generation failed: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
