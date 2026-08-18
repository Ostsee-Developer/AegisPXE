package debian13

import (
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/installation"
)

func TestRenderBootIsDeterministicAndSecretFree(t *testing.T) {
	spec := validInstallationSpec()
	first, err := RenderBoot(spec)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RenderBoot(spec)
	if err != nil {
		t.Fatal(err)
	}
	if first.InstallationID != spec.ID || second.InstallationID != spec.ID {
		t.Fatal("boot spec lost installation identity")
	}
	if first.Kernel.Digest != second.Kernel.Digest || first.Initrds[0].Digest != second.Initrds[0].Digest || first.SeedRef != second.SeedRef {
		t.Fatalf("boot rendering is not deterministic: first=%+v second=%+v", first, second)
	}
	for _, arg := range first.Arguments {
		lower := strings.ToLower(arg.Key + "=" + arg.Value)
		if strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "credential") {
			t.Fatalf("secret-looking boot argument rendered: %+v", arg)
		}
	}
}

func TestRenderBootRejectsMixedInstallerVersions(t *testing.T) {
	spec := validInstallationSpec()
	spec.Artifacts[1].Version = "installer-2"
	if _, err := RenderBoot(spec); err == nil {
		t.Fatal("expected mixed installer versions to be rejected")
	}
}

func validInstallationSpec() installation.Spec {
	return installation.Spec{
		ID:                    "i_test",
		MachineID:             "m_test",
		DriverID:              driverID,
		DriverVersion:         "0.1.0-dev.3",
		OSRelease:             "13",
		Architecture:          debianArch,
		ProfileID:             "standard",
		ProfileRevision:       "rev_standard_1",
		Artifacts: []installation.Artifact{
			{
				ID:         "debian13-amd64-netboot-linux",
				Name:       "linux",
				SourceURL:  "https://deb.debian.org/debian/dists/trixie/main/installer-amd64/installer-1/images/netboot/debian-installer/amd64/linux",
				Version:    "installer-1",
				Digest:     testDigest("a"),
				Size:       1,
				Provenance: "debian:trixie:release=13.6:installer=installer-1",
			},
			{
				ID:         "debian13-amd64-netboot-initrd",
				Name:       "initrd.gz",
				SourceURL:  "https://deb.debian.org/debian/dists/trixie/main/installer-amd64/installer-1/images/netboot/debian-installer/amd64/initrd.gz",
				Version:    "installer-1",
				Digest:     testDigest("b"),
				Size:       1,
				Provenance: "debian:trixie:release=13.6:installer=installer-1",
			},
		},
		Storage:               installation.Storage{Mode: "whole-disk", Filesystem: "ext4"},
		Security:              installation.Security{AutomaticSecurityUpdates: true},
		LifecycleCredentialID: "cred_test",
		CreatedBy:             "system:test",
	}
}

func testDigest(value string) string {
	return "sha256:" + strings.Repeat(value, 64)
}
