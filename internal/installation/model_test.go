package installation

import "testing"

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

func TestSpecValidationRejectsDuplicateArtifactRoles(t *testing.T) {
	spec := validSpec()
	duplicate := spec.Artifacts[0]
	duplicate.ID = "artifact_linux_duplicate"
	spec.Artifacts = append(spec.Artifacts, duplicate)
	if err := spec.Validate(); err == nil {
		t.Fatal("expected duplicate artifact role to be rejected")
	}
}

func TestCloneOwnsArtifactSlice(t *testing.T) {
	spec := validSpec()
	clone := spec.Clone()
	clone.Artifacts[0].Digest = "sha256:" + repeatHex("b")
	if spec.Artifacts[0].Digest == clone.Artifacts[0].Digest {
		t.Fatal("clone shares mutable artifact slice")
	}
}

func validSpec() Spec {
	return Spec{
		MachineID:       "m_test",
		DriverID:        "debian13",
		DriverVersion:   "0.1.0-dev.1",
		OSRelease:       "13",
		Architecture:    "amd64",
		ProfileID:       "standard",
		ProfileRevision: "rev_1",
		Artifacts: []Artifact{{
			ID:         "artifact_linux",
			Name:       "linux",
			SourceURL:  "https://deb.debian.org/debian/dists/trixie/example/linux",
			Version:    "installer-1",
			Digest:     "sha256:" + repeatHex("a"),
			Size:       1,
			Provenance: "debian:trixie:test",
		}},
		Storage:               Storage{Mode: "whole-disk", Filesystem: "ext4"},
		Security:              Security{AutomaticSecurityUpdates: true},
		LifecycleCredentialID: "cred_test",
		CreatedBy:             "system:test",
	}
}

func repeatHex(value string) string {
	out := ""
	for len(out) < 64 {
		out += value
	}
	return out[:64]
}
