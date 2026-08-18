package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

var ErrIntegrityMismatch = errors.New("artifact integrity mismatch")

type Descriptor struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	SourceURL  string `json:"source_url"`
	Version    string `json:"version"`
	Digest     string `json:"digest"`
	Size       int64  `json:"size"`
	Provenance string `json:"provenance"`
}

type Verified struct {
	Descriptor Descriptor
	Content    []byte
}

func (d Descriptor) Validate() error {
	for name, value := range map[string]string{
		"artifact ID":         d.ID,
		"artifact name":       d.Name,
		"artifact source URL": d.SourceURL,
		"artifact version":    d.Version,
		"artifact provenance": d.Provenance,
	} {
		if strings.TrimSpace(value) == "" {
			return errors.New(name + " is required")
		}
	}
	if len(d.ID) > 128 || len(d.Name) > 128 || len(d.Version) > 128 || len(d.Provenance) > 256 || len(d.SourceURL) > 2048 {
		return errors.New("artifact metadata exceeds size limit")
	}
	u, err := url.Parse(d.SourceURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return errors.New("artifact source URL must be an absolute HTTPS URL without user info")
	}
	if !ValidSHA256(d.Digest) {
		return errors.New("artifact digest must be canonical sha256")
	}
	if d.Size <= 0 {
		return errors.New("artifact size must be positive")
	}
	return nil
}

func VerifyContent(d Descriptor, content []byte) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if int64(len(content)) != d.Size {
		return fmt.Errorf("%w: size mismatch", ErrIntegrityMismatch)
	}
	if SHA256(content) != d.Digest {
		return fmt.Errorf("%w: sha256 mismatch", ErrIntegrityMismatch)
	}
	return nil
}

func SHA256(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func ValidSHA256(value string) bool {
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	raw := strings.TrimPrefix(value, prefix)
	if len(raw) != 64 || strings.ToLower(raw) != raw {
		return false
	}
	_, err := hex.DecodeString(raw)
	return err == nil
}
