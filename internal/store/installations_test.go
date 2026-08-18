package store

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/installation"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
)

func TestInstallationSpecRoundTripIsImmutableSnapshot(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	machineRecord, _, err := store.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:10:01"}, "req_discover")
	if err != nil {
		t.Fatal(err)
	}
	input := installation.Spec{
		MachineID:             machineRecord.ID,
		DriverID:              "debian13",
		DriverVersion:         "0.1.0-dev.1",
		OSRelease:             "13",
		Architecture:          "amd64",
		ProfileID:             "standard",
		ProfileRevision:       "rev_standard_1",
		Artifacts:             []installation.Artifact{installationArtifact("linux", "a"), installationArtifact("initrd.gz", "b")},
		Storage:               installation.Storage{Mode: "whole-disk", Filesystem: "ext4"},
		Security:              installation.Security{AutomaticSecurityUpdates: true},
		LifecycleCredentialID: "cred_1",
		CreatedBy:             "system:test",
	}
	created, err := store.CreateInstallationSpec(ctx, input, "req_install")
	if err != nil {
		t.Fatal(err)
	}
	input.Artifacts[0].Digest = digest("c")

	loaded, err := store.InstallationSpec(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Artifacts[0].Digest != digest("a") {
		t.Fatalf("stored spec mutated through caller-owned slice: %+v", loaded.Artifacts)
	}
	if loaded.ID == "" || loaded.CreatedAt.IsZero() || loaded.CreatedBy != "system:test" {
		t.Fatalf("missing immutable creation metadata: %+v", loaded)
	}
	if !reflect.DeepEqual(created, loaded) {
		t.Fatalf("round trip mismatch:\ncreated=%+v\nloaded=%+v", created, loaded)
	}

	events, err := store.Events(ctx, event.EntityInstallation, created.ID)
	if err != nil || len(events) != 1 || events[0].Type != event.InstallationCreated || events[0].Actor != "system:test" {
		t.Fatalf("unexpected installation audit timeline: %+v err=%v", events, err)
	}
}

func TestInstallationSpecRejectsCallerAssignedIdentity(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	machineRecord, _, err := store.DiscoverMachine(ctx, machine.Observation{MAC: "BC:24:11:00:10:02"}, "req_discover")
	if err != nil {
		t.Fatal(err)
	}
	spec := installation.Spec{
		ID:                    "i_caller",
		MachineID:             machineRecord.ID,
		DriverID:              "debian13",
		DriverVersion:         "0.1.0-dev.1",
		OSRelease:             "13",
		Architecture:          "amd64",
		ProfileID:             "standard",
		ProfileRevision:       "rev_standard_1",
		Artifacts:             []installation.Artifact{installationArtifact("linux", "a")},
		Storage:               installation.Storage{Mode: "whole-disk", Filesystem: "ext4"},
		LifecycleCredentialID: "cred_1",
		CreatedBy:             "system:test",
	}
	_, err = store.CreateInstallationSpec(ctx, spec, "req_install")
	if fault.Code(err) != fault.InstallationSpecInvalid {
		t.Fatalf("caller-assigned identity code=%q err=%v", fault.Code(err), err)
	}
}

func TestInstallationSpecRequiresExistingMachine(t *testing.T) {
	store := testStore(t)
	_, err := store.CreateInstallationSpec(context.Background(), installation.Spec{
		MachineID:             "m_missing",
		DriverID:              "debian13",
		DriverVersion:         "0.1.0-dev.1",
		OSRelease:             "13",
		Architecture:          "amd64",
		ProfileID:             "standard",
		ProfileRevision:       "rev_standard_1",
		Artifacts:             []installation.Artifact{installationArtifact("linux", "a")},
		Storage:               installation.Storage{Mode: "whole-disk", Filesystem: "ext4"},
		LifecycleCredentialID: "cred_1",
		CreatedBy:             "system:test",
	}, "req_install")
	if fault.Code(err) != fault.MachineNotFound {
		t.Fatalf("missing-machine code=%q err=%v", fault.Code(err), err)
	}
}

func installationArtifact(name, value string) installation.Artifact {
	return installation.Artifact{
		ID:         "artifact_" + strings.TrimSuffix(name, ".gz"),
		Name:       name,
		SourceURL:  "https://deb.debian.org/debian/dists/trixie/example/" + name,
		Version:    "installer-1",
		Digest:     digest(value),
		Size:       1,
		Provenance: "debian:trixie:test",
	}
}

func digest(value string) string {
	return "sha256:" + strings.Repeat(value, 64)
}
