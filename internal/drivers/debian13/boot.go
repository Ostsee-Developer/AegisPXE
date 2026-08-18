package debian13

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Ostsee-Developer/AegisPXE/internal/boot"
	"github.com/Ostsee-Developer/AegisPXE/internal/installation"
)

const driverID = "debian13"

func RenderBoot(spec installation.Spec) (boot.Spec, error) {
	if err := spec.Validate(); err != nil {
		return boot.Spec{}, fmt.Errorf("invalid installation spec: %w", err)
	}
	if spec.ID == "" {
		return boot.Spec{}, errors.New("installation spec must have a server-assigned ID before boot rendering")
	}
	if spec.DriverID != driverID {
		return boot.Spec{}, errors.New("installation spec is assigned to a different driver")
	}
	if spec.OSRelease != "13" || spec.Architecture != debianArch {
		return boot.Spec{}, errors.New("installation target is not Debian 13 amd64")
	}

	kernel, err := requiredArtifact(spec, "linux")
	if err != nil {
		return boot.Spec{}, err
	}
	initrd, err := requiredArtifact(spec, "initrd.gz")
	if err != nil {
		return boot.Spec{}, err
	}
	if kernel.Version != initrd.Version {
		return boot.Spec{}, errors.New("Debian kernel and initrd are pinned to different installer versions")
	}
	if kernel.Provenance != initrd.Provenance {
		return boot.Spec{}, errors.New("Debian kernel and initrd have different provenance")
	}

	result := boot.Spec{
		InstallationID: spec.ID,
		DriverID:       spec.DriverID,
		DriverVersion:  spec.DriverVersion,
		Kernel: boot.ArtifactRef{
			ID:     kernel.ID,
			Name:   kernel.Name,
			Digest: kernel.Digest,
		},
		Initrds: []boot.ArtifactRef{{
			ID:     initrd.ID,
			Name:   initrd.Name,
			Digest: initrd.Digest,
		}},
		Arguments: []boot.Argument{
			{Key: "auto", Value: "true"},
			{Key: "priority", Value: "critical"},
			{Key: "interface", Value: "auto"},
		},
		SeedRef: "installation/" + spec.ID + "/debian-preseed",
	}
	if err := result.Validate(); err != nil {
		return boot.Spec{}, fmt.Errorf("rendered boot spec is invalid: %w", err)
	}
	return result.Clone(), nil
}

func requiredArtifact(spec installation.Spec, name string) (installation.Artifact, error) {
	var found installation.Artifact
	count := 0
	for _, item := range spec.Artifacts {
		if item.Name == name {
			found = item
			count++
		}
	}
	if count != 1 {
		return installation.Artifact{}, fmt.Errorf("installation spec must contain exactly one %s artifact", name)
	}
	if !strings.HasPrefix(found.Provenance, "debian:trixie:") {
		return installation.Artifact{}, fmt.Errorf("%s artifact is not trusted Debian trixie provenance", name)
	}
	return found, nil
}
