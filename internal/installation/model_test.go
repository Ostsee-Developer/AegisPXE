package installation

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/profile"
)

func TestSpecValidationRejectsUnverifiedArtifactDigest(t *testing.T) {
	spec := validSpec()
	spec.Artifacts[0].Digest = "sha256:not-a-digest"
	if err := spec.Validate(); err == nil {
		t.Fatal("expected invalid artifact digest")
	}
}

func TestSpecValidationRejectsTPMWithoutEncryption(t *testing.T) {
	spec := validSpec()
	spec.Storage.TPM2 = true
	spec.Storage.Encrypted = false
	if err := spec.Validate(); err == nil {
		t.Fatal("expected invalid TPM2 storage combination")
	}
}

func TestSpecValidationRejectsPartitionAsTargetDisk(t *testing.T) {
	spec := validSpec()
	spec.Storage.TargetDisk = "/dev/vda1"
	if err := spec.Validate(); err == nil {
		t.Fatal("expected partition target to be rejected")
	}
}

func TestSpecValidationRejectsDuplicateArtifactRoles(t *testing.T) {
	spec := validSpec()
	duplicate := spec.Artifacts[0]
	duplicate.ID = "artifact_linux_duplicate"
	spec.Artifacts = append(spec.Artifacts, duplicate)
	if err := spec.Validate(); err == nil {
		t.Fatal("expected duplicate artifact role to be rejected")
	}
}

func TestCloneOwnsMutableSlices(t *testing.T) {
	spec := validSpec()
	clone := spec.Clone()
	clone.Artifacts[0].Digest = "sha256:" + repeatHex("b")
	clone.Profile.Packages[0] = "curl"
	if spec.Artifacts[0].Digest == clone.Artifacts[0].Digest || spec.Profile.Packages[0] == clone.Profile.Packages[0] {
		t.Fatal("installation spec clone shares mutable slices")
	}
}

func validSpec() Spec {
	return Spec{
		MachineID:       "m_test",
		DriverID:        "debian13",
		DriverVersion:   "1",
		OSRelease:       "13",
		Architecture:    "amd64",
		ProfileID:       "standard",
		ProfileRevision: "rev_1",
		Profile: profile.Snapshot{
			SchemaVersion: profile.SchemaVersion,
			Hostname:      "aegis-node",
			Locale:        "de_DE.UTF-8",
			Keyboard:      "de",
			Timezone:      "Europe/Berlin",
			Admin: profile.Admin{
				Username:          "guardian",
				FullName:          "Aegis Administrator",
				AuthorizedSSHKeys: []string{validPublicKey()},
			},
			Packages: []string{"jq"},
		},
		Artifacts: []Artifact{{
			ID:         "artifact_linux",
			Name:       "linux",
			SourceURL:  "https://deb.debian.org/debian/dists/trixie/example/linux",
			Version:    "installer-1",
			Digest:     "sha256:" + repeatHex("a"),
			Size:       1,
			Provenance: "debian:trixie:test",
		}},
		Storage:               Storage{Mode: "whole-disk", Filesystem: "ext4", TargetDisk: "/dev/vda"},
		Security:              Security{AutomaticSecurityUpdates: true},
		LifecycleCredentialID: "cred_test",
		CreatedBy:             "system:test",
	}
}

func validPublicKey() string {
	payload := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 64)))
	return "ssh-ed25519 " + payload + " test"
}

func repeatHex(value string) string {
	out := ""
	for len(out) < 64 {
		out += value
	}
	return out[:64]
}
