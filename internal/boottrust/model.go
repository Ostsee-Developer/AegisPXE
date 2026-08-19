package boottrust

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

type KeyState string

const (
	KeyPending  KeyState = "pending"
	KeyApproved KeyState = "approved"
	KeyRevoked  KeyState = "revoked"
)

const (
	MaxPublicKeyPEMBytes = 8 << 10
	MaxEKFingerprintLen  = 128
)

type Key struct {
	Fingerprint   string
	MachineID     string
	PublicKeyPEM  string
	EKFingerprint string
	State         KeyState
	FirstSeenAt   time.Time
	LastSeenAt    time.Time
	ApprovedAt    time.Time
	ApprovedBy    string
	RevokedAt     time.Time
	RevokedBy     string
}

type Challenge struct {
	ID               string
	InstallationID   string
	MachineID        string
	KeyFingerprint   string
	Nonce            []byte
	CreatedAt        time.Time
	ExpiresAt        time.Time
	UsedAt           time.Time
	ResponseCipher   []byte
	CredentialExpiry time.Time
}

type Release struct {
	ChallengeID      string
	Ciphertext       []byte
	Algorithm        string
	CredentialExpiry time.Time
	Duplicate        bool
}

func ParseRSAPublicKeyPEM(value string) (*rsa.PublicKey, string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > MaxPublicKeyPEMBytes {
		return nil, "", errors.New("boot trust public key is missing or too large")
	}
	block, rest := pem.Decode([]byte(value))
	if block == nil || len(strings.TrimSpace(string(rest))) != 0 || block.Type != "PUBLIC KEY" {
		return nil, "", errors.New("boot trust key must be one PKIX PUBLIC KEY PEM block")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, "", fmt.Errorf("parse boot trust public key: %w", err)
	}
	key, ok := parsed.(*rsa.PublicKey)
	if !ok || key.N == nil || key.E != 65537 || key.N.BitLen() < 2048 || key.N.BitLen() > 4096 {
		return nil, "", errors.New("boot trust key must be RSA 2048-4096 with exponent 65537")
	}
	fingerprint := sha256.Sum256(block.Bytes)
	return key, "sha256:" + hex.EncodeToString(fingerprint[:]), nil
}

func ValidateEKFingerprint(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if len(value) > MaxEKFingerprintLen || !strings.HasPrefix(value, "sha256:") {
		return errors.New("EK fingerprint is invalid")
	}
	hexValue := strings.TrimPrefix(value, "sha256:")
	if len(hexValue) != 64 {
		return errors.New("EK fingerprint is invalid")
	}
	if _, err := hex.DecodeString(hexValue); err != nil {
		return errors.New("EK fingerprint is invalid")
	}
	return nil
}

func CanonicalChallenge(c Challenge) ([]byte, error) {
	if strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.InstallationID) == "" || strings.TrimSpace(c.MachineID) == "" || strings.TrimSpace(c.KeyFingerprint) == "" || len(c.Nonce) != 32 {
		return nil, errors.New("boot trust challenge is incomplete")
	}
	payload := strings.Join([]string{
		"AEGISPXE-BOOT-TRUST-V1",
		c.ID,
		c.InstallationID,
		c.MachineID,
		c.KeyFingerprint,
		base64.RawURLEncoding.EncodeToString(c.Nonce),
	}, "\n") + "\n"
	return []byte(payload), nil
}
