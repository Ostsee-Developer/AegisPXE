package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/artifact"
	"github.com/Ostsee-Developer/AegisPXE/internal/assignment"
	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/installation"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
	"github.com/Ostsee-Developer/AegisPXE/internal/observability"
	"github.com/Ostsee-Developer/AegisPXE/internal/profile"
	"github.com/Ostsee-Developer/AegisPXE/internal/store"
)

func TestArmedDiscoveryChainsProvisioningWithoutConsumingAssignment(t *testing.T) {
	state, handler := testServer(t)
	ctx := context.Background()
	form := url.Values{
		"mac":          {"52:54:00:40:00:01"},
		"smbios_uuid":  {"40000000-0000-4000-8000-000000000001"},
		"architecture": {"x86_64"},
		"firmware":     {"efi"},
		"secure_boot":  {"01"},
		"setup_mode":   {"00"},
	}

	req := httptest.NewRequest(http.MethodGet, "http://aegispxe.test/api/v1/discovery.ipxe?"+form.Encode(), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("initial discovery status=%d body=%s", rec.Code, rec.Body.String())
	}
	machines, err := state.Machines(ctx)
	if err != nil || len(machines) != 1 {
		t.Fatalf("machines=%+v err=%v", machines, err)
	}
	if _, err := state.SetMachinePolicy(ctx, machines[0].ID, machine.PolicyProvision, "req_approve", "test:operator"); err != nil {
		t.Fatal(err)
	}
	spec := createProvisioningSpec(t, state, machines[0].ID, "req_create_spec")
	armed, err := state.ArmInstallation(ctx, machines[0].ID, spec.ID, "req_arm", "test:operator")
	if err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(http.MethodGet, "http://aegispxe.test/api/v1/discovery.ipxe?"+form.Encode(), nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("armed discovery status=%d body=%s", rec.Code, rec.Body.String())
	}
	want := "chain http://aegispxe.test/boot/installations/" + spec.ID + "/boot.ipxe"
	if !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("armed discovery did not chain provisioner: %s", rec.Body.String())
	}
	current, err := state.AssignmentForInstallation(ctx, spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != armed.ID || current.State != assignment.StateArmed || !current.ConsumedAt.IsZero() {
		t.Fatalf("discovery changed assignment: %+v", current)
	}
}

func TestInstallationBootScriptUsesNativeInitrdPreseedAndDebianShimWithoutSecrets(t *testing.T) {
	state, handler := testServer(t)
	machineRecord, spec := createArmedProvisioningState(t, state, "52:54:00:40:00:02")

	req := httptest.NewRequest(http.MethodGet, "http://aegispxe.test/boot/installations/"+spec.ID+"/boot.ipxe", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("boot script status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"kernel http://aegispxe.test/boot/installations/" + spec.ID + "/artifacts/linux auto=true priority=critical interface=auto",
		"initrd http://aegispxe.test/boot/installations/" + spec.ID + "/artifacts/initrd.gz",
		"initrd http://aegispxe.test/boot/installations/" + spec.ID + "/preseed.cfg /preseed.cfg",
		"shim http://aegispxe.test/boot/installations/" + spec.ID + "/artifacts/bootnetx64.efi || goto secure_boot_failed",
		"boot || goto boot_failed",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("boot script missing %q: %s", want, body)
		}
	}
	for _, forbidden := range []string{"reporter", "overlay.cpio", "initrd.img", "initrd=", "--name", "initrd.magic"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("boot script contains suspended transport fragment %q: %s", forbidden, body)
		}
	}
	if strings.Contains(strings.ToLower(body), "preseed/url") || strings.Contains(body, spec.LifecycleCredentialID) || strings.Contains(strings.ToLower(body), "token=") {
		t.Fatal("boot script contains network-preseed or credential material")
	}
	current, err := state.ActiveAssignmentForMachine(context.Background(), machineRecord.ID)
	if err != nil || current.State != assignment.StateArmed {
		t.Fatalf("boot script read changed assignment: %+v err=%v", current, err)
	}
}

func TestAssignmentGatedArtifactReadUsesPinnedDescriptorWithoutLifecycleMutation(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	state, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "aegispxe.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	_, spec := createArmedProvisioningState(t, state, "52:54:00:40:00:03")
	loader := &recordingArtifactLoader{content: []byte("verified-kernel")}
	server := New(state, logger, "test")
	mux := http.NewServeMux()
	server.registerProvisioningWithLoader(mux, loader)
	handler := requestLog(logger, mux)

	req := httptest.NewRequest(http.MethodGet, "http://aegispxe.test/boot/installations/"+spec.ID+"/artifacts/linux", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "verified-kernel" {
		t.Fatalf("artifact status=%d body=%q", rec.Code, rec.Body.String())
	}
	if loader.descriptor.Name != "linux" || loader.installationID != spec.ID || loader.requestID == "" {
		t.Fatalf("loader did not receive pinned/correlated descriptor: %+v", loader)
	}
	events, err := state.Events(context.Background(), event.EntityInstallation, spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Type != event.InstallationCreated || events[1].Type != event.InstallationArmed {
		t.Fatalf("artifact read mutated installation lifecycle: %+v", events)
	}
}

func TestProvisioningReadLogsCorrelationWithoutCredentialMetadata(t *testing.T) {
	var logs bytes.Buffer
	logger := observability.New(&logs, slog.LevelDebug)
	state, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "aegispxe.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	machineRecord, spec := createArmedProvisioningState(t, state, "52:54:00:40:00:04")
	server := New(state, logger, "test")
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodGet, "http://aegispxe.test/boot/installations/"+spec.ID+"/preseed.cfg", nil)
	req.Header.Set("X-Request-ID", "req_public_preseed")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("preseed status=%d body=%s", rec.Code, rec.Body.String())
	}
	logText := logs.String()
	for _, want := range []string{
		`"component":"httpapi.provisioning"`,
		`"operation":"serve_preseed"`,
		`"request_id":"req_public_preseed"`,
		`"machine_id":"` + machineRecord.ID + `"`,
		`"installation_id":"` + spec.ID + `"`,
		`"result":"success"`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("provisioning log missing %s: %s", want, logText)
		}
	}
	if strings.Contains(logText, spec.LifecycleCredentialID) || strings.Contains(logText, strings.Fields(spec.Profile.Admin.AuthorizedSSHKeys[0])[1]) {
		t.Fatal("provisioning read log leaked credential or SSH key")
	}
}

type recordingArtifactLoader struct {
	content        []byte
	descriptor     artifact.Descriptor
	requestID      string
	installationID string
}

func (l *recordingArtifactLoader) Load(_ context.Context, descriptor artifact.Descriptor, requestID, installationID string) ([]byte, error) {
	l.descriptor = descriptor
	l.requestID = requestID
	l.installationID = installationID
	return append([]byte(nil), l.content...), nil
}

func createArmedProvisioningState(t *testing.T, state *store.Store, mac string) (machine.Machine, installation.Spec) {
	t.Helper()
	ctx := context.Background()
	item, _, err := state.DiscoverMachine(ctx, machine.Observation{
		MAC: mac, Architecture: "x86_64", Firmware: "efi", SecureBoot: "01", SetupMode: "00",
	}, "req_discover_fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.SetMachinePolicy(ctx, item.ID, machine.PolicyProvision, "req_approve_fixture", "test:operator"); err != nil {
		t.Fatal(err)
	}
	spec := createProvisioningSpec(t, state, item.ID, "req_create_fixture")
	if _, err := state.ArmInstallation(ctx, item.ID, spec.ID, "req_arm_fixture", "test:operator"); err != nil {
		t.Fatal(err)
	}
	updated, err := state.Machine(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	return updated, spec
}

func createProvisioningSpec(t *testing.T, state *store.Store, machineID, requestID string) installation.Spec {
	t.Helper()
	keyPayload := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 64)))
	provenance := "debian:trixie:release=13.6:installer=installer-1"
	base := "https://deb.debian.org/debian/dists/trixie/main/installer-amd64/installer-1/images/netboot/debian-installer/amd64/"
	spec, err := state.CreateInstallationSpec(context.Background(), installation.Spec{
		MachineID:       machineID,
		DriverID:        "debian13",
		DriverVersion:   "2",
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
				AuthorizedSSHKeys: []string{"ssh-ed25519 " + keyPayload + " test"},
				PasswordlessSudo:  true,
			},
			Packages: []string{"jq"},
		},
		Artifacts: []installation.Artifact{
			{ID: "debian13-amd64-netboot-linux", Name: "linux", SourceURL: base + "linux", Version: "installer-1", Digest: "sha256:" + strings.Repeat("a", 64), Size: 16, Provenance: provenance},
			{ID: "debian13-amd64-netboot-initrd", Name: "initrd.gz", SourceURL: base + "initrd.gz", Version: "installer-1", Digest: "sha256:" + strings.Repeat("b", 64), Size: 16, Provenance: provenance},
			{ID: "debian13-amd64-netboot-shim", Name: "bootnetx64.efi", SourceURL: base + "bootnetx64.efi", Version: "installer-1", Digest: "sha256:" + strings.Repeat("c", 64), Size: 16, Provenance: provenance},
		},
		Storage:               installation.Storage{Mode: "whole-disk", Filesystem: "ext4", TargetDisk: "/dev/vda"},
		Security:              installation.Security{AutomaticSecurityUpdates: true},
		LifecycleCredentialID: "cred_fixture_must_not_leak",
		CreatedBy:             "test:operator",
	}, requestID)
	if err != nil {
		t.Fatal(err)
	}
	return spec
}
