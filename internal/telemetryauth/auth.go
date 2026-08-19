package telemetryauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

const Scheme = "Aegis-HMAC-SHA256"

func KeyFromSecret(secret string) [32]byte {
	return sha256.Sum256([]byte(secret))
}

func Canonical(method, path, idempotencyKey string, unixSeconds int64, body []byte) ([]byte, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	path = strings.TrimSpace(path)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if method == "" || path == "" || !strings.HasPrefix(path, "/") || idempotencyKey == "" || unixSeconds <= 0 {
		return nil, errors.New("telemetry authentication input is incomplete")
	}
	bodyDigest := sha256.Sum256(body)
	value := strings.Join([]string{
		"AEGISPXE-TELEMETRY-AUTH-V1",
		method,
		path,
		idempotencyKey,
		strconv.FormatInt(unixSeconds, 10),
		hex.EncodeToString(bodyDigest[:]),
	}, "\n") + "\n"
	return []byte(value), nil
}

func Sign(key []byte, canonical []byte) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(canonical)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func Verify(key []byte, canonical []byte, encodedSignature string) bool {
	provided, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(encodedSignature))
	if err != nil || len(provided) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(canonical)
	return hmac.Equal(provided, mac.Sum(nil))
}
