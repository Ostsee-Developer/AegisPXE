package boot

import (
	"errors"
	"regexp"
	"strings"

	"github.com/Ostsee-Developer/AegisPXE/internal/artifact"
)

var argumentKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_.\/-]{1,128}$`)

type ArtifactRef struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Digest string `json:"digest"`
}

type Argument struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Spec struct {
	InstallationID string        `json:"installation_id"`
	DriverID       string        `json:"driver_id"`
	DriverVersion  string        `json:"driver_version"`
	Kernel         ArtifactRef   `json:"kernel"`
	Initrds        []ArtifactRef `json:"initrds"`
	Arguments      []Argument    `json:"arguments"`
	SeedRef        string        `json:"seed_ref"`
}

func (s Spec) Validate() error {
	if strings.TrimSpace(s.InstallationID) == "" || strings.TrimSpace(s.DriverID) == "" || strings.TrimSpace(s.DriverVersion) == "" {
		return errors.New("boot spec identity is incomplete")
	}
	if len(s.InstallationID) > 128 || len(s.DriverID) > 64 || len(s.DriverVersion) > 64 {
		return errors.New("boot spec identity exceeds size limit")
	}
	if err := validateArtifactRef(s.Kernel); err != nil {
		return errors.New("boot spec kernel is invalid: " + err.Error())
	}
	if len(s.Initrds) == 0 || len(s.Initrds) > 4 {
		return errors.New("boot spec must contain between one and four initrds")
	}
	seenArtifacts := map[string]struct{}{s.Kernel.ID: {}}
	for _, item := range s.Initrds {
		if err := validateArtifactRef(item); err != nil {
			return errors.New("boot spec initrd is invalid: " + err.Error())
		}
		if _, exists := seenArtifacts[item.ID]; exists {
			return errors.New("boot spec contains duplicate artifact reference")
		}
		seenArtifacts[item.ID] = struct{}{}
	}
	if len(s.Arguments) > 32 {
		return errors.New("boot spec contains too many kernel arguments")
	}
	seenArguments := make(map[string]struct{}, len(s.Arguments))
	for _, arg := range s.Arguments {
		if !argumentKeyPattern.MatchString(arg.Key) {
			return errors.New("boot spec kernel argument key is invalid")
		}
		if len(arg.Value) > 512 || strings.ContainsAny(arg.Value, "\r\n\x00") {
			return errors.New("boot spec kernel argument value is invalid")
		}
		lower := strings.ToLower(arg.Key)
		if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "credential") {
			return errors.New("boot spec kernel arguments may not carry secrets")
		}
		if _, exists := seenArguments[arg.Key]; exists {
			return errors.New("boot spec contains duplicate kernel argument")
		}
		seenArguments[arg.Key] = struct{}{}
	}
	if strings.TrimSpace(s.SeedRef) == "" || len(s.SeedRef) > 256 || strings.ContainsAny(s.SeedRef, "\r\n\x00?#") || strings.Contains(s.SeedRef, "://") {
		return errors.New("boot spec seed reference is invalid")
	}
	return nil
}

func (s Spec) Clone() Spec {
	copy := s
	copy.Initrds = append([]ArtifactRef(nil), s.Initrds...)
	copy.Arguments = append([]Argument(nil), s.Arguments...)
	return copy
}

func validateArtifactRef(ref ArtifactRef) error {
	if strings.TrimSpace(ref.ID) == "" || strings.TrimSpace(ref.Name) == "" {
		return errors.New("artifact ID and name are required")
	}
	if len(ref.ID) > 128 || len(ref.Name) > 128 {
		return errors.New("artifact reference exceeds size limit")
	}
	if !artifact.ValidSHA256(ref.Digest) {
		return errors.New("artifact digest must be canonical sha256")
	}
	return nil
}
