package boot

import (
	"strings"
	"testing"
)

func TestSpecRejectsSecretKernelArgument(t *testing.T) {
	spec := validBootSpec()
	spec.Arguments = append(spec.Arguments, Argument{Key: "aegis_token", Value: "secret-value"})
	if err := spec.Validate(); err == nil {
		t.Fatal("expected secret-bearing kernel argument to be rejected")
	}
}

func TestSpecCloneOwnsSlices(t *testing.T) {
	spec := validBootSpec()
	clone := spec.Clone()
	clone.Initrds[0].Digest = digestHex("c")
	clone.Arguments[0].Value = "false"
	if clone.Initrds[0].Digest == spec.Initrds[0].Digest || clone.Arguments[0].Value == spec.Arguments[0].Value {
		t.Fatal("boot spec clone shares mutable slices")
	}
}

func validBootSpec() Spec {
	return Spec{
		InstallationID: "i_test",
		DriverID:       "debian13",
		DriverVersion:  "0.1.0-dev.3",
		Kernel: ArtifactRef{
			ID:     "debian13-amd64-netboot-linux",
			Name:   "linux",
			Digest: digestHex("a"),
		},
		Initrds: []ArtifactRef{{
			ID:     "debian13-amd64-netboot-initrd",
			Name:   "initrd.gz",
			Digest: digestHex("b"),
		}},
		Arguments: []Argument{{Key: "auto", Value: "true"}},
		SeedRef:   "installation/i_test/debian-preseed",
	}
}

func digestHex(value string) string {
	return "sha256:" + strings.Repeat(value, 64)
}
