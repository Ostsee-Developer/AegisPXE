package debian13

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/installation"
	"github.com/Ostsee-Developer/AegisPXE/internal/profile"
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

func TestValidateSpecAcceptsDraftButRenderRequiresServerIdentity(t *testing.T) {
	spec := validInstallationSpec()
	spec.ID = ""
	if err := ValidateSpec(spec); err != nil {
		t.Fatalf("draft capability validation failed: %v", err)
	}
	if _, err := RenderBoot(spec); err == nil {
		t.Fatal("rendering accepted an InstallationSpec without server-assigned identity")
	}
}

func TestRenderBootRejectsMixedInstallerVersions(t *testing.T) {
	spec := validInstallationSpec()
	spec.Artifacts[1].Version = "installer-2"
	if _, err := RenderBoot(spec); err == nil {
		t.Fatal("expected mixed installer versions to be rejected")
	}
}

func TestValidateSpecRejectsArtifactOutsideTrustedDebianOrigin(t *testing.T) {
	spec := validInstallationSpec()
	spec.Artifacts[0].SourceURL = "https://example.invalid/debian/dists/trixie/main/installer-amd64/installer-1/images/netboot/debian-installer/amd64/linux"
	if err := ValidateSpec(spec); err == nil {
		t.Fatal("expected untrusted artifact origin to be rejected")
	}
}

func TestValidateSpecRejectsDifferentDriverContractVersion(t *testing.T) {
	spec := validInstallationSpec()
	spec.DriverVersion = "999"
	if err := ValidateSpec(spec); err == nil {
		t.Fatal("expected unsupported driver contract version to be rejected")
	}
}

func TestValidateSpecRejectsPasswordSSHForStandard(t *testing.T) {
	spec := validInstallationSpec()
	spec.Security.SSHPasswordAuthentication = true
	if err := ValidateSpec(spec); err == nil {
		t.Fatal("expected password SSH to be rejected by Debian Standard")
	}
}

func TestValidateSpecRejectsMissingSecureBootShimInV2(t *testing.T) {
	spec := validInstallationSpec()
	spec.Artifacts = spec.Artifacts[:2]
	if err := ValidateSpec(spec); err == nil {
		t.Fatal("expected driver v2 without signed Debian shim to be rejected")
	}
}

func TestValidateSpecKeepsLegacyV1Readable(t *testing.T) {
	spec := validInstallationSpec()
	spec.DriverVersion = LegacyDriverVersion
	spec.Artifacts = spec.Artifacts[:2]
	if err := ValidateSpec(spec); err != nil {
		t.Fatalf("legacy driver-v1 InstallationSpec should remain readable: %v", err)
	}
}

func validInstallationSpec() installation.Spec {
	keyPayload := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 64)))
	provenance := "debian:trixie:release=13.6:installer=installer-1"
	base := "https://deb.debian.org/debian/dists/trixie/main/installer-amd64/installer-1/images/netboot/debian-installer/amd64/"
	return installation.Spec{
		ID:              "i_test",
		MachineID:       "m_test",
		DriverID:        DriverID,
		DriverVersion:   DriverVersion,
		OSRelease:       "13",
		Architecture:    debianArch,
		ProfileID:       "standard",
		ProfileRevision: "rev_standard_1",
		Profile: profile.Snapshot{
			SchemaVersion: profile.SchemaVersion,
			Hostname:      "aegis-node",
			Locale:        "de_DE.UTF-8",
			Keyboard:      "de",
			Timezone:      "Europe/Berlin",
			Admin: profile.Admin{
				Username:          "guardian",
				FullName:          "Aegis Administrator",
				AuthorizedSSHKeys: []string{"ssh-ed25519 " + keyPayload + " test"},
				PasswordlessSudo:  true,
			},
			Packages: []string{"jq"},
		},
		Artifacts: []installation.Artifact{
			{ID: "debian13-amd64-netboot-linux", Name: "linux", SourceURL: base + "linux", Version: "installer-1", Digest: testDigest("a"), Size: 1, Provenance: provenance},
			{ID: "debian13-amd64-netboot-initrd", Name: "initrd.gz", SourceURL: base + "initrd.gz", Version: "installer-1", Digest: testDigest("b"), Size: 1, Provenance: provenance},
			{ID: "debian13-amd64-netboot-shim", Name: "bootnetx64.efi", SourceURL: base + "bootnetx64.efi", Version: "installer-1", Digest: testDigest("c"), Size: 1, Provenance: provenance},
		},
		Storage:               installation.Storage{Mode: "whole-disk", Filesystem: "ext4", TargetDisk: "/dev/vda"},
		Security:              installation.Security{AutomaticSecurityUpdates: true},
		LifecycleCredentialID: "cred_test",
		CreatedBy:             "system:test",
	}
}

func testDigest(value string) string {
	return "sha256:" + strings.Repeat(value, 64)
}
