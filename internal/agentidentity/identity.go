package agentidentity

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/Ostsee-Developer/AegisPXE/internal/agent"
)

const (
	SchemaVersion   = 1
	maxIdentitySize = 64 * 1024
)

var (
	trailerMagic      = []byte("AEGISPXE-AGENT-ID-V1")
	instanceIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{7,127}$`)
)

type Identity struct {
	SchemaVersion      int      `json:"schema_version"`
	AgentID            string   `json:"agent_id"`
	InstallationID     string   `json:"installation_id"`
	MachineID          string   `json:"machine_id"`
	InstanceID         string   `json:"instance_id"`
	ControllerURL      string   `json:"controller_url"`
	Version            string   `json:"version"`
	Generation         int      `json:"generation"`
	Architecture       string   `json:"architecture"`
	CapabilityCeiling  []string `json:"capability_ceiling"`
	ServerCAPEM        string   `json:"server_ca_pem"`
	UpdateVerifyKeyB64 string   `json:"update_verify_key"`
}

func (i Identity) Normalize() (Identity, error) {
	i.AgentID = strings.TrimSpace(i.AgentID)
	i.InstallationID = strings.TrimSpace(i.InstallationID)
	i.MachineID = strings.TrimSpace(i.MachineID)
	i.InstanceID = strings.TrimSpace(i.InstanceID)
	i.ControllerURL = strings.TrimSpace(i.ControllerURL)
	i.Version = strings.TrimSpace(i.Version)
	i.Architecture = strings.TrimSpace(i.Architecture)
	i.ServerCAPEM = strings.TrimSpace(i.ServerCAPEM)
	i.UpdateVerifyKeyB64 = strings.TrimSpace(i.UpdateVerifyKeyB64)
	capabilities, err := agent.NormalizeCapabilityCeiling(i.CapabilityCeiling)
	if err != nil {
		return Identity{}, err
	}
	i.CapabilityCeiling = capabilities
	if err := i.Validate(); err != nil {
		return Identity{}, err
	}
	return i, nil
}

func (i Identity) Validate() error {
	if i.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported agent identity schema %d", i.SchemaVersion)
	}
	if err := agent.ValidateID(i.AgentID); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"installation ID": i.InstallationID,
		"machine ID":      i.MachineID,
		"version":         i.Version,
	} {
		if value == "" || value != strings.TrimSpace(value) || len(value) > 128 {
			return fmt.Errorf("%s is invalid", name)
		}
	}
	if !instanceIDPattern.MatchString(i.InstanceID) {
		return errors.New("AegisPXE instance ID is invalid")
	}
	controller, err := url.Parse(i.ControllerURL)
	if err != nil || controller.Scheme != "https" || controller.Host == "" || controller.User != nil || controller.RawQuery != "" || controller.Fragment != "" || (controller.Path != "" && controller.Path != "/") {
		return errors.New("agent controller URL must be an HTTPS origin")
	}
	if i.Generation < 1 {
		return errors.New("agent generation must be positive")
	}
	if i.Architecture != "amd64" && i.Architecture != "arm64" {
		return fmt.Errorf("unsupported agent architecture %q", i.Architecture)
	}
	normalized, err := agent.NormalizeCapabilityCeiling(i.CapabilityCeiling)
	if err != nil {
		return err
	}
	if len(normalized) != len(i.CapabilityCeiling) {
		return errors.New("agent capability ceiling is not normalized")
	}
	for index := range normalized {
		if normalized[index] != i.CapabilityCeiling[index] {
			return errors.New("agent capability ceiling is not normalized")
		}
	}
	block, rest := pem.Decode([]byte(i.ServerCAPEM))
	if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		return errors.New("agent server CA must contain exactly one PEM certificate")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil || !certificate.IsCA {
		return errors.New("agent server CA certificate is invalid")
	}
	verifyKey, err := base64.RawURLEncoding.DecodeString(i.UpdateVerifyKeyB64)
	if err != nil || len(verifyKey) != ed25519.PublicKeySize {
		return errors.New("agent update verification key is invalid")
	}
	return nil
}

func Seal(template []byte, identity Identity) ([]byte, error) {
	if len(template) == 0 {
		return nil, errors.New("agent template is empty")
	}
	if HasIdentity(template) {
		return nil, errors.New("agent template is already sealed")
	}
	normalized, err := identity.Normalize()
	if err != nil {
		return nil, fmt.Errorf("normalize agent identity: %w", err)
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("encode agent identity: %w", err)
	}
	if len(payload) == 0 || len(payload) > maxIdentitySize {
		return nil, errors.New("agent identity payload exceeds size limit")
	}
	digest := sha256.Sum256(payload)
	out := make([]byte, 0, len(template)+len(payload)+sha256.Size+4+len(trailerMagic))
	out = append(out, template...)
	out = append(out, payload...)
	out = append(out, digest[:]...)
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(payload)))
	out = append(out, length[:]...)
	out = append(out, trailerMagic...)
	return out, nil
}

func Read(data []byte) (Identity, error) {
	minimum := len(trailerMagic) + 4 + sha256.Size + 1
	if len(data) < minimum || !bytes.Equal(data[len(data)-len(trailerMagic):], trailerMagic) {
		return Identity{}, errors.New("managed agent identity trailer is missing")
	}
	lengthOffset := len(data) - len(trailerMagic) - 4
	payloadLength := int(binary.BigEndian.Uint32(data[lengthOffset : lengthOffset+4]))
	if payloadLength <= 0 || payloadLength > maxIdentitySize {
		return Identity{}, errors.New("managed agent identity trailer length is invalid")
	}
	digestOffset := lengthOffset - sha256.Size
	payloadOffset := digestOffset - payloadLength
	if payloadOffset < 0 {
		return Identity{}, errors.New("managed agent identity trailer is truncated")
	}
	payload := data[payloadOffset:digestOffset]
	digest := sha256.Sum256(payload)
	if !bytes.Equal(digest[:], data[digestOffset:lengthOffset]) {
		return Identity{}, errors.New("managed agent identity digest mismatch")
	}
	var identity Identity
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&identity); err != nil {
		return Identity{}, fmt.Errorf("decode managed agent identity: %w", err)
	}
	if err := identity.Validate(); err != nil {
		return Identity{}, fmt.Errorf("validate managed agent identity: %w", err)
	}
	return identity, nil
}

func ReadFile(path string) (Identity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Identity{}, err
	}
	return Read(data)
}

func ReadExecutable() (Identity, error) {
	path, err := os.Executable()
	if err != nil {
		return Identity{}, err
	}
	return ReadFile(path)
}

func HasIdentity(data []byte) bool {
	return len(data) >= len(trailerMagic) && bytes.Equal(data[len(data)-len(trailerMagic):], trailerMagic)
}
