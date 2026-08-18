package webui

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/assignment"
	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/installation"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
	"github.com/Ostsee-Developer/AegisPXE/internal/profile"
)

func TestInstallationDetailShowsTrustWithoutCredentialOrSSHKeyMaterial(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	machineRecord := machine.Machine{ID: "m_test", Policy: machine.PolicyProvision, Architecture: "amd64", Firmware: "efi", FirstSeen: now, LastSeen: now}
	spec := installation.Spec{
		ID:              "i_test",
		MachineID:       machineRecord.ID,
		DriverID:        "debian13",
		DriverVersion:   "1",
		OSRelease:       "13",
		Architecture:    "amd64",
		ProfileID:       "standard",
		ProfileRevision: "rev_1",
		Profile: profile.Snapshot{
			SchemaVersion: profile.SchemaVersion,
			Hostname:      "node-01",
			Locale:        "de_DE.UTF-8",
			Keyboard:      "de",
			Timezone:      "Europe/Berlin",
			Admin: profile.Admin{
				Username:          "guardian",
				FullName:          "Aegis Administrator",
				AuthorizedSSHKeys: []string{"ssh-ed25519 PUBLIC_KEY_MUST_NOT_RENDER"},
				PasswordlessSudo:  true,
			},
			Packages: []string{"jq"},
		},
		Artifacts: []installation.Artifact{{ID: "kernel", Name: "linux", Digest: "sha256:" + strings.Repeat("a", 64), Size: 4096, Version: "installer-1", Provenance: "debian:trixie:test"}},
		Storage:               installation.Storage{Mode: "whole-disk", Filesystem: "ext4", TargetDisk: "/dev/vda"},
		Security:              installation.Security{AutomaticSecurityUpdates: true},
		LifecycleCredentialID: "cred_must_not_render",
		CreatedAt:             now,
		CreatedBy:             "test:operator",
	}
	assignmentRecord := assignment.Assignment{ID: "a_test", MachineID: machineRecord.ID, InstallationID: spec.ID, State: assignment.StateArmed, TrustRequirement: assignment.TrustRequirementCryptographic, ArmedAt: now, ArmedBy: "test:operator"}
	store := &fakeInstallationStore{
		machine:    machineRecord,
		spec:       spec,
		assignment: assignmentRecord,
		events: []event.Event{{EntityType: event.EntityInstallation, EntityID: spec.ID, Type: event.InstallationArmed, OccurredAt: now, Actor: "test:operator", RequestID: "req_test", Message: "installation armed"}},
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	ui := New(store, logger, "test")
	mux := http.NewServeMux()
	ui.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "http://aegispxe.test/ui/installations/i_test", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Cryptographic boot trust", "Secret release", "BLOCKED", "/dev/vda", "INSTALLATION_ARMED", "1 pinned public key(s)"} {
		if !strings.Contains(body, want) {
			t.Fatalf("installation page missing %q", want)
		}
	}
	if strings.Contains(body, "PUBLIC_KEY_MUST_NOT_RENDER") || strings.Contains(body, "cred_must_not_render") {
		t.Fatal("installation page leaked credential metadata or SSH key material")
	}
}

type fakeInstallationStore struct {
	machine    machine.Machine
	spec       installation.Spec
	assignment assignment.Assignment
	events     []event.Event
}

func (s *fakeInstallationStore) Machines(context.Context) ([]machine.Machine, error) {
	return []machine.Machine{s.machine}, nil
}

func (s *fakeInstallationStore) Machine(_ context.Context, id string) (machine.Machine, error) {
	if id != s.machine.ID {
		return machine.Machine{}, fault.New(fault.MachineNotFound, "machine not found", nil)
	}
	return s.machine, nil
}

func (s *fakeInstallationStore) MachineIdentifiers(context.Context, string) ([]machine.Identifier, error) {
	return nil, nil
}

func (s *fakeInstallationStore) Events(_ context.Context, entityType, entityID string) ([]event.Event, error) {
	if entityType == event.EntityInstallation && entityID == s.spec.ID {
		return append([]event.Event(nil), s.events...), nil
	}
	return nil, nil
}

func (s *fakeInstallationStore) InstallationSpecs(context.Context) ([]installation.Spec, error) {
	return []installation.Spec{s.spec.Clone()}, nil
}

func (s *fakeInstallationStore) InstallationSpec(_ context.Context, id string) (installation.Spec, error) {
	if id != s.spec.ID {
		return installation.Spec{}, fault.New(fault.InstallationNotFound, "installation not found", nil)
	}
	return s.spec.Clone(), nil
}

func (s *fakeInstallationStore) AssignmentForInstallation(_ context.Context, installationID string) (assignment.Assignment, error) {
	if installationID != s.assignment.InstallationID {
		return assignment.Assignment{}, fault.New(fault.InstallationAssignmentNotFound, "assignment not found", nil)
	}
	return s.assignment, nil
}
