package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/artifact"
	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/installation"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
	"github.com/Ostsee-Developer/AegisPXE/internal/observability"
	"github.com/Ostsee-Developer/AegisPXE/internal/profile"
	"github.com/Ostsee-Developer/AegisPXE/internal/store"
)

func TestArtifactServingDoesNotAdvanceInstallationLifecycle(t *testing.T) {
	var logs bytes.Buffer
	logger := observability.New(&logs, slog.LevelDebug)
	state, err := store.Open(context.Background(), t.TempDir()+"/aegispxe.db", logger)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()

	machineRecord, _, err := state.DiscoverMachine(context.Background(), machine.Observation{MAC: "BC:24:11:00:20:01"}, "req_discover_artifact")
	if err != nil {
		t.Fatal(err)
	}
	kernelContent := []byte("verified-kernel")
	initrdContent := []byte("verified-initrd")
	created, err := state.CreateInstallationSpec(context.Background(), installation.Spec{
		MachineID:       machineRecord.ID,
		DriverID:        "debian13",
		DriverVersion:   "1",
		OSRelease:       "13",
		Architecture:    "amd64",
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
				AuthorizedSSHKeys: []string{artifactServingPublicKey()},
				PasswordlessSudo:  true,
			},
			Packages: []string{"jq"},
		},
		Artifacts: []installation.Artifact{
			{
				ID:         "debian13-amd64-netboot-linux",
				Name:       "linux",
				SourceURL:  "https://deb.debian.org/debian/dists/trixie/main/installer-amd64/installer-1/images/netboot/debian-installer/amd64/linux",
				Version:    "installer-1",
				Digest:     artifact.SHA256(kernelContent),
				Size:       int64(len(kernelContent)),
				Provenance: "debian:trixie:release=13.6:installer=installer-1",
			},
			{
				ID:         "debian13-amd64-netboot-initrd",
				Name:       "initrd.gz",
				SourceURL:  "https://deb.debian.org/debian/dists/trixie/main/installer-amd64/installer-1/images/netboot/debian-installer/amd64/initrd.gz",
				Version:    "installer-1",
				Digest:     artifact.SHA256(initrdContent),
				Size:       int64(len(initrdContent)),
				Provenance: "debian:trixie:release=13.6:installer=installer-1",
			},
		},
		Storage:               installation.Storage{Mode: "whole-disk", Filesystem: "ext4", TargetDisk: "/dev/vda"},
		Security:              installation.Security{AutomaticSecurityUpdates: true},
		LifecycleCredentialID: "cred_artifact_test",
		CreatedBy:             "system:test",
	}, "req_create_artifact")
	if err != nil {
		t.Fatal(err)
	}

	loader := &staticArtifactLoader{content: kernelContent}
	handler := withArtifactServing(http.NotFoundHandler(), state, logger, loader)
	req := httptest.NewRequest(http.MethodGet, "http://aegispxe.test/boot/installations/"+created.ID+"/artifacts/linux", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), kernelContent) {
		t.Fatalf("artifact body mismatch: %q", rec.Body.Bytes())
	}
	if loader.descriptor.ID != "debian13-amd64-netboot-linux" || loader.installationID != created.ID || loader.requestID == "" {
		t.Fatalf("loader correlation mismatch: %+v", loader)
	}

	events, err := state.Events(context.Background(), event.EntityInstallation, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != event.InstallationCreated {
		t.Fatalf("artifact fetch mutated lifecycle: %+v", events)
	}
	if strings.Contains(logs.String(), string(kernelContent)) || !strings.Contains(logs.String(), `"operation":"serve_verified_artifact"`) || !strings.Contains(logs.String(), `"installation_id":"`+created.ID+`"`) {
		t.Fatalf("artifact serving log is incomplete or leaked content: %s", logs.String())
	}
}

type staticArtifactLoader struct {
	content        []byte
	descriptor     artifact.Descriptor
	requestID      string
	installationID string
}

func (l *staticArtifactLoader) Load(_ context.Context, descriptor artifact.Descriptor, requestID, installationID string) ([]byte, error) {
	l.descriptor = descriptor
	l.requestID = requestID
	l.installationID = installationID
	return append([]byte(nil), l.content...), nil
}

func artifactServingPublicKey() string {
	payload := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 64)))
	return "ssh-ed25519 " + payload + " test"
}
