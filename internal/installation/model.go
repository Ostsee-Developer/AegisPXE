package installation

import (
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

type Artifact struct {
	ID     string
	Name   string
	Digest string
}

type Storage struct {
	Mode       string
	Filesystem string
	Encrypted  bool
	TPM2       bool
}

type Security struct {
	SSHPasswordAuthentication bool
	RootLogin                 bool
	AutomaticSecurityUpdates  bool
}

type Spec struct {
	ID                    string
	MachineID             string
	DriverID              string
	DriverVersion         string
	OSRelease             string
	Architecture          string
	ProfileID             string
	ProfileRevision       string
	Artifacts             []Artifact
	Storage               Storage
	Security              Security
	LifecycleCredentialID string
	CreatedAt             time.Time
	CreatedBy             string
}

func (s Spec) Validate() error {
	for name, value := range map[string]string{
		"machine ID":              s.MachineID,
		"driver ID":               s.DriverID,
		"driver version":          s.DriverVersion,
		"OS release":              s.OSRelease,
		"architecture":            s.Architecture,
		"profile ID":              s.ProfileID,
		"profile revision":        s.ProfileRevision,
		"lifecycle credential ID": s.LifecycleCredentialID,
		"created by":              s.CreatedBy,
	} {
		if strings.TrimSpace(value) == "" {
			return errors.New(name + " is required")
		}
	}
	if len(s.DriverID) > 64 || len(s.DriverVersion) > 64 || len(s.OSRelease) > 64 || len(s.Architecture) > 64 {
		return errors.New("installation target metadata exceeds size limit")
	}
	if len(s.ProfileID) > 128 || len(s.ProfileRevision) > 128 || len(s.LifecycleCredentialID) > 128 || len(s.CreatedBy) > 128 {
		return errors.New("installation metadata exceeds size limit")
	}
	if len(s.Artifacts) == 0 {
		return errors.New("at least one verified artifact is required")
	}
	if len(s.Artifacts) > 16 {
		return errors.New("too many installation artifacts")
	}
	for _, artifact := range s.Artifacts {
		if strings.TrimSpace(artifact.ID) == "" || strings.TrimSpace(artifact.Name) == "" {
			return errors.New("artifact ID and name are required")
		}
		if len(artifact.ID) > 128 || len(artifact.Name) > 128 {
			return errors.New("artifact metadata exceeds size limit")
		}
		if !validSHA256(artifact.Digest) {
			return errors.New("artifact digest must be canonical sha256")
		}
	}
	if strings.TrimSpace(s.Storage.Mode) == "" || strings.TrimSpace(s.Storage.Filesystem) == "" {
		return errors.New("storage mode and filesystem are required")
	}
	if len(s.Storage.Mode) > 64 || len(s.Storage.Filesystem) > 64 {
		return errors.New("storage metadata exceeds size limit")
	}
	if s.Storage.TPM2 && !s.Storage.Encrypted {
		return errors.New("TPM2 enrollment requires encrypted storage")
	}
	return nil
}

func (s Spec) Clone() Spec {
	copy := s
	copy.Artifacts = append([]Artifact(nil), s.Artifacts...)
	return copy
}

func validSHA256(value string) bool {
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
