package debian13

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/Ostsee-Developer/AegisPXE/internal/boot"
	"github.com/Ostsee-Developer/AegisPXE/internal/installation"
)

func ValidateSpec(spec installation.Spec) error {
	if err := spec.Validate(); err != nil {
		return fmt.Errorf("invalid installation spec: %w", err)
	}
	if spec.ID == "" {
		return errors.New("installation spec must have a server-assigned ID before boot rendering")
	}
	if spec.DriverID != DriverID {
		return errors.New("installation spec is assigned to a different driver")
	}
	if spec.DriverVersion != DriverVersion {
		return errors.New("installation spec requires a different Debian driver contract version")
	}
	if spec.OSRelease != "13" || spec.Architecture != debianArch {
		return errors.New("installation target is not Debian 13 amd64")
	}
	if len(spec.Artifacts) != 2 {
		return errors.New("Debian 13 boot requires exactly kernel and initrd artifacts")
	}

	kernel, err := requiredArtifact(spec, "linux")
	if err != nil {
		return err
	}
	initrd, err := requiredArtifact(spec, "initrd.gz")
	if err != nil {
		return err
	}
	if kernel.Version != initrd.Version {
		return errors.New("Debian kernel and initrd are pinned to different installer versions")
	}
	if kernel.Provenance != initrd.Provenance {
		return errors.New("Debian kernel and initrd have different provenance")
	}
	if err := validateDebianArtifactSource(kernel, "netboot/debian-installer/amd64/linux"); err != nil {
		return fmt.Errorf("kernel artifact source is invalid: %w", err)
	}
	if err := validateDebianArtifactSource(initrd, "netboot/debian-installer/amd64/initrd.gz"); err != nil {
		return fmt.Errorf("initrd artifact source is invalid: %w", err)
	}
	return nil
}

func RenderBoot(spec installation.Spec) (boot.Spec, error) {
	if err := ValidateSpec(spec); err != nil {
		return boot.Spec{}, err
	}
	kernel, _ := requiredArtifact(spec, "linux")
	initrd, _ := requiredArtifact(spec, "initrd.gz")

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

func validateDebianArtifactSource(item installation.Artifact, suffix string) error {
	u, err := url.Parse(item.SourceURL)
	if err != nil {
		return err
	}
	if u.Scheme != "https" || u.Host != debianHost || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("source must be the trusted Debian HTTPS origin without query or fragment")
	}
	expected := "/debian/dists/trixie/main/installer-amd64/" + item.Version + "/images/" + suffix
	if u.EscapedPath() != expected && u.Path != expected {
		return errors.New("source path does not match pinned installer version and artifact role")
	}
	return nil
}
