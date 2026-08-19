package reporter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/boottrust"
	"github.com/Ostsee-Developer/AegisPXE/internal/lifecycle"
	"github.com/Ostsee-Developer/AegisPXE/internal/telemetryauth"
)

var ErrPendingApproval = errors.New("TPM boot trust key awaits operator approval")

type Client struct {
	config Config
	http   *http.Client
}

type CredentialMaterial struct {
	Secret     string
	Ciphertext []byte
	ExpiresAt  time.Time
	Fingerprint string
}

func NewClient(config Config) *Client {
	return &Client{config: config, http: &http.Client{Timeout: 20 * time.Second}}
}

func (c *Client) AcquireCredential(ctx context.Context, key *TPMKey) (CredentialMaterial, error) {
	var enrolled struct {
		Fingerprint string `json:"fingerprint"`
		State       string `json:"state"`
	}
	if err := c.postJSON(ctx, c.config.endpoint("/trust/enroll"), map[string]any{
		"public_key_pem": key.PublicKeyPEM(),
	}, &enrolled); err != nil {
		return CredentialMaterial{}, err
	}
	if enrolled.Fingerprint != key.Fingerprint() {
		return CredentialMaterial{}, errors.New("server returned a different boot trust fingerprint")
	}
	if enrolled.State != string(boottrust.KeyApproved) {
		return CredentialMaterial{}, ErrPendingApproval
	}

	var challenge struct {
		ID             string `json:"challenge_id"`
		InstallationID string `json:"installation_id"`
		MachineID      string `json:"machine_id"`
		Fingerprint    string `json:"fingerprint"`
		Nonce          string `json:"nonce"`
	}
	if err := c.postJSON(ctx, c.config.endpoint("/trust/challenge"), map[string]string{"fingerprint": key.Fingerprint()}, &challenge); err != nil {
		return CredentialMaterial{}, err
	}
	nonce, err := base64.RawURLEncoding.DecodeString(challenge.Nonce)
	if err != nil {
		return CredentialMaterial{}, fmt.Errorf("decode boot trust challenge: %w", err)
	}
	canonical, err := boottrust.CanonicalChallenge(boottrust.Challenge{
		ID: challenge.ID, InstallationID: challenge.InstallationID, MachineID: challenge.MachineID,
		KeyFingerprint: challenge.Fingerprint, Nonce: nonce,
	})
	if err != nil {
		return CredentialMaterial{}, err
	}
	if challenge.InstallationID != c.config.InstallationID || challenge.MachineID != c.config.MachineID || challenge.Fingerprint != key.Fingerprint() {
		return CredentialMaterial{}, errors.New("boot trust challenge binding does not match reporter configuration")
	}
	signature, err := key.Sign(canonical)
	if err != nil {
		return CredentialMaterial{}, err
	}
	var release struct {
		ChallengeID string    `json:"challenge_id"`
		Algorithm   string    `json:"algorithm"`
		Ciphertext  string    `json:"ciphertext"`
		ExpiresAt   time.Time `json:"credential_expires"`
	}
	if err := c.postJSON(ctx, c.config.endpoint("/trust/prove"), map[string]string{
		"challenge_id": challenge.ID,
		"signature":    base64.RawURLEncoding.EncodeToString(signature),
	}, &release); err != nil {
		return CredentialMaterial{}, err
	}
	if release.ChallengeID != challenge.ID || release.Algorithm != "RSA-OAEP-SHA256" {
		return CredentialMaterial{}, errors.New("server returned an unsupported trust release")
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(release.Ciphertext)
	if err != nil {
		return CredentialMaterial{}, fmt.Errorf("decode encrypted lifecycle credential: %w", err)
	}
	secret, err := key.DecryptLifecycleCredential(ciphertext)
	if err != nil {
		return CredentialMaterial{}, err
	}
	return CredentialMaterial{Secret: secret, Ciphertext: ciphertext, ExpiresAt: release.ExpiresAt, Fingerprint: key.Fingerprint()}, nil
}

func (c *Client) PostEvent(ctx context.Context, secret, idempotencyKey string, stage lifecycle.Stage, source lifecycle.Source, message, errorCode string, clientTime time.Time) error {
	payload := map[string]any{
		"stage":   stage,
		"source":  source,
		"message": strings.TrimSpace(message),
	}
	if !clientTime.IsZero() {
		payload["client_time"] = clientTime.UTC().Format(time.RFC3339Nano)
	}
	if strings.TrimSpace(errorCode) != "" {
		payload["error_code"] = strings.TrimSpace(errorCode)
	}
	return c.postSigned(ctx, secret, idempotencyKey, "/reporter/events", payload)
}

func (c *Client) PostLog(ctx context.Context, secret, idempotencyKey string, sequence int64, source lifecycle.Source, content string, clientTime time.Time) error {
	payload := map[string]any{
		"sequence": sequence,
		"source":   source,
		"content":  content,
	}
	if !clientTime.IsZero() {
		payload["client_time"] = clientTime.UTC().Format(time.RFC3339Nano)
	}
	return c.postSigned(ctx, secret, idempotencyKey, "/reporter/logs", payload)
}

func (c *Client) postSigned(ctx context.Context, secret, idempotencyKey, suffix string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	urlValue := c.config.endpoint(suffix)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlValue, bytes.NewReader(body))
	if err != nil {
		return err
	}
	timestamp := time.Now().UTC().Unix()
	canonical, err := telemetryauth.Canonical(http.MethodPost, req.URL.Path, idempotencyKey, timestamp, body)
	if err != nil {
		return err
	}
	key := telemetryauth.KeyFromSecret(secret)
	signature := telemetryauth.Sign(key[:], canonical)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)
	req.Header.Set("X-Aegis-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("Authorization", telemetryauth.Scheme+" "+signature)
	return c.do(req, nil)
}

func (c *Client) postJSON(ctx context.Context, urlValue string, payload any, destination any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlValue, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, destination)
}

func (c *Client) do(req *http.Request, destination any) error {
	response, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 256<<10))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var apiError struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &apiError)
		if apiError.Error.Code != "" {
			return fmt.Errorf("%s: %s", apiError.Error.Code, apiError.Error.Message)
		}
		return fmt.Errorf("AegisPXE API returned HTTP %d", response.StatusCode)
	}
	if destination != nil {
		decoder := json.NewDecoder(bytes.NewReader(body))
		if err := decoder.Decode(destination); err != nil {
			return err
		}
	}
	return nil
}

func CredentialVerifier(secret string) [32]byte { return sha256.Sum256([]byte(secret)) }
