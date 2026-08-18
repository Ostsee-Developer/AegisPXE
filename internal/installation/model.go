package installation

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/artifact"
)

type Artifact = artifact.Descriptor

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
	seenIDs := make(map[string]struct{}, len(s.Artifacts))
	seenNames := make(map[string]struct{}, len(s.Artifacts))
	for _, item := range s.Artifacts {
		if err := item.Validate(); err != nil {
			return fmt.Errorf("invalid installation artifact %q: %w", item.ID, err)
		}
		if _, exists := seenIDs[item.ID]; exists {
			return errors.New("installation artifacts must have unique IDs")
		}
		if _, exists := seenNames[item.Name]; exists {
			return errors.New("installation artifacts must have unique names")
		}
		seenIDs[item.ID] = struct{}{}
		seenNames[item.Name] = struct{}{}
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
