package agentbuild

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Ostsee-Developer/AegisPXE/internal/agent"
)

const ManifestSchemaVersion = 1

type Manifest struct {
	SchemaVersion     int      `json:"schema_version"`
	AgentID           string   `json:"agent_id"`
	InstallationID    string   `json:"installation_id"`
	MachineID         string   `json:"machine_id"`
	InstanceID        string   `json:"instance_id"`
	Version           string   `json:"version"`
	Generation        int      `json:"generation"`
	Architecture      string   `json:"architecture"`
	CapabilityCeiling []string `json:"capability_ceiling"`
	PackageSHA256     string   `json:"package_sha256"`
	PackageSize       int64    `json:"package_size"`
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("unsupported agent build manifest schema %d", m.SchemaVersion)
	}
	if err := agent.ValidateID(m.AgentID); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"installation ID": m.InstallationID,
		"machine ID":      m.MachineID,
		"instance ID":     m.InstanceID,
		"version":         m.Version,
	} {
		if value == "" || value != strings.TrimSpace(value) || len(value) > 128 {
			return fmt.Errorf("%s is invalid", name)
		}
	}
	if m.Generation < 1 {
		return errors.New("agent build generation must be positive")
	}
	if m.Architecture != "amd64" && m.Architecture != "arm64" {
		return fmt.Errorf("unsupported agent architecture %q", m.Architecture)
	}
	capabilities, err := agent.NormalizeCapabilityCeiling(m.CapabilityCeiling)
	if err != nil {
		return err
	}
	if len(capabilities) != len(m.CapabilityCeiling) {
		return errors.New("agent build capability ceiling is not normalized")
	}
	for i := range capabilities {
		if capabilities[i] != m.CapabilityCeiling[i] {
			return errors.New("agent build capability ceiling is not normalized")
		}
	}
	if len(m.PackageSHA256) != sha256.Size*2 {
		return errors.New("agent package SHA-256 is invalid")
	}
	if _, err := hex.DecodeString(m.PackageSHA256); err != nil {
		return errors.New("agent package SHA-256 is invalid")
	}
	if m.PackageSize <= 0 {
		return errors.New("agent package size must be positive")
	}
	return nil
}

func (m Manifest) CanonicalJSON() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("encode agent build manifest: %w", err)
	}
	return payload, nil
}

func Digest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
