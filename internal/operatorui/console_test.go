package operatorui

import (
	"bytes"
	"context"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/artifact"
	"github.com/Ostsee-Developer/AegisPXE/internal/drivers/debian13"
	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
	"github.com/Ostsee-Developer/AegisPXE/internal/observability"
	"github.com/Ostsee-Developer/AegisPXE/internal/store"
)

type fakeDebianResolver struct {
	resolution debian13.Resolution
	err        error
	calls      int
}

func (r *fakeDebianResolver) Resolve(context.Context) (debian13.Resolution, error) {
	r.calls++
	return r.resolution, r.err
}

func TestOperatorConsoleShowsAuthenticatedControlsWithoutSessionToken(t *testing.T) {
	key, auth := testAuth(t)
	var logs bytes.Buffer
	logger := observability.New(&logs, slog.LevelDebug)
	state, item := operatorTestStore(t, logger)
	defer state.Close()
	handler := New(http.NotFoundHandler(), state, auth, logger)
	cookie, session := loginOperator(t, handler, auth, key)

	req := httptest.NewRequest(http.MethodGet, "https://aegispxe.test/ui/operator/", nil)
	req.RemoteAddr = "192.0.2.12:40123"
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Operator Console", item.ID, session.CSRFToken, "Create InstallationSpec", "Machine policy"} {
		if !strings.Contains(body, want) {
			t.Fatalf("console missing %q", want)
		}
	}
	if strings.Contains(body, cookie.Value) || strings.Contains(body, key) {
		t.Fatal("console leaked bootstrap key or session token")
	}
}

func TestInstallationWizardCreatesImmutableVerifiedDebianSpec(t *testing.T) {
	key, auth := testAuth(t)
	var logs bytes.Buffer
	logger := observability.New(&logs, slog.LevelDebug)
	state, item := operatorTestStore(t, logger)
	defer state.Close()
	handler := New(http.NotFoundHandler(), state, auth, logger)
	base := handler.(*Handler)
	resolver := &fakeDebianResolver{resolution: testDebianResolution()}
	base.resolver = resolver
	cookie, session := loginOperator(t, handler, auth, key)
	sshKey := testPublicKey()

	form := url.Values{
		"csrf":            {session.CSRFToken},
		"machine_id":      {item.ID},
		"hostname":        {"node-01"},
		"locale":          {"de_DE.UTF-8"},
		"keyboard":        {"de"},
		"timezone":        {"Europe/Berlin"},
		"admin_username":  {"guardian"},
		"admin_full_name": {"Aegis Administrator"},
		"ssh_keys":        {sshKey},
		"packages":        {"curl, nano qemu-guest-agent"},
		"target_disk":     {"/dev/vda"},
	}
	req := httptest.NewRequest(http.MethodPost, "https://aegispxe.test/ui/operator/installations", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Request-ID", "req_operator_wizard")
	req.RemoteAddr = "192.0.2.12:40123"
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/ui/installations/i_") {
		t.Fatalf("status=%d location=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if resolver.calls != 1 {
		t.Fatalf("resolver calls=%d want=1", resolver.calls)
	}
	specs, err := state.InstallationSpecs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 {
		t.Fatalf("spec count=%d want=1", len(specs))
	}
	spec := specs[0]
	if spec.MachineID != item.ID || spec.DriverID != debian13.DriverID || spec.DriverVersion != debian13.DriverVersion || spec.OSRelease != "13" || spec.Architecture != "amd64" {
		t.Fatalf("installation target mismatch: %+v", spec)
	}
	if spec.Profile.Hostname != "node-01" || spec.Profile.Admin.Username != "guardian" || len(spec.Profile.Admin.AuthorizedSSHKeys) != 1 || spec.Storage.TargetDisk != "/dev/vda" {
		t.Fatalf("installation snapshot mismatch: %+v", spec)
	}
	if spec.CreatedBy != "bootstrap:operator" || spec.ID == "" || spec.CreatedAt.IsZero() || spec.LifecycleCredentialID == "" {
		t.Fatalf("server-owned installation metadata missing: %+v", spec)
	}
	events, err := state.Events(context.Background(), event.EntityInstallation, spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != event.InstallationCreated || events[0].Actor != "bootstrap:operator" || events[0].RequestID != "req_operator_wizard" {
		t.Fatalf("installation creation audit mismatch: %+v", events)
	}
	logText := logs.String()
	if !strings.Contains(logText, `"operation":"create_spec"`) || !strings.Contains(logText, `"request_id":"req_operator_wizard"`) || !strings.Contains(logText, `"result":"success"`) {
		t.Fatalf("wizard success log missing correlation: %s", logText)
	}
	keyPayload := strings.Fields(sshKey)[1]
	if strings.Contains(logText, keyPayload) || strings.Contains(logText, cookie.Value) || strings.Contains(logText, session.CSRFToken) || strings.Contains(logText, key) {
		t.Fatal("wizard logs leaked SSH key or operator credentials")
	}
}

func TestInstallationWizardRejectsPartitionBeforeArtifactResolution(t *testing.T) {
	key, auth := testAuth(t)
	logger := observability.New(&bytes.Buffer{}, slog.LevelDebug)
	state, item := operatorTestStore(t, logger)
	defer state.Close()
	handler := New(http.NotFoundHandler(), state, auth, logger)
	base := handler.(*Handler)
	resolver := &fakeDebianResolver{resolution: testDebianResolution()}
	base.resolver = resolver
	cookie, session := loginOperator(t, handler, auth, key)

	form := url.Values{
		"csrf":            {session.CSRFToken},
		"machine_id":      {item.ID},
		"hostname":        {"node-01"},
		"locale":          {"de_DE.UTF-8"},
		"keyboard":        {"de"},
		"timezone":        {"Europe/Berlin"},
		"admin_username":  {"guardian"},
		"admin_full_name": {"Aegis Administrator"},
		"ssh_keys":        {testPublicKey()},
		"target_disk":     {"/dev/vda1"},
	}
	req := httptest.NewRequest(http.MethodPost, "https://aegispxe.test/ui/operator/installations", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "192.0.2.12:40123"
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Target disk is invalid") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if resolver.calls != 0 {
		t.Fatalf("artifact resolver called %d times for invalid target disk", resolver.calls)
	}
	specs, err := state.InstallationSpecs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 0 {
		t.Fatalf("invalid wizard request persisted %d specs", len(specs))
	}
}

func operatorTestStore(t *testing.T, logger *slog.Logger) (*store.Store, machine.Machine) {
	t.Helper()
	state, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "aegispxe.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	item, _, err := state.DiscoverMachine(context.Background(), machine.Observation{MAC: "BC:24:11:00:80:01", Architecture: "x86_64", Firmware: "efi"}, "req_discover_console")
	if err != nil {
		state.Close()
		t.Fatal(err)
	}
	item, err = state.SetMachinePolicy(context.Background(), item.ID, machine.PolicyProvision, "req_approve_console", "bootstrap:operator")
	if err != nil {
		state.Close()
		t.Fatal(err)
	}
	return state, item
}

func testDebianResolution() debian13.Resolution {
	const version = "20250803+deb13u6"
	const provenance = "debian:trixie:release=13.6:installer=20250803+deb13u6"
	kernelContent := []byte("verified-kernel")
	initrdContent := []byte("verified-initrd")
	return debian13.Resolution{
		ReleaseVersion:   "13.6",
		InstallerVersion: version,
		Kernel: artifact.Verified{
			Descriptor: artifact.Descriptor{
				ID:         "debian13-amd64-netboot-linux",
				Name:       "linux",
				SourceURL:  "https://deb.debian.org/debian/dists/trixie/main/installer-amd64/" + version + "/images/netboot/debian-installer/amd64/linux",
				Version:    version,
				Digest:     artifact.SHA256(kernelContent),
				Size:       int64(len(kernelContent)),
				Provenance: provenance,
			},
			Content: kernelContent,
		},
		Initrd: artifact.Verified{
			Descriptor: artifact.Descriptor{
				ID:         "debian13-amd64-netboot-initrd",
				Name:       "initrd.gz",
				SourceURL:  "https://deb.debian.org/debian/dists/trixie/main/installer-amd64/" + version + "/images/netboot/debian-installer/amd64/initrd.gz",
				Version:    version,
				Digest:     artifact.SHA256(initrdContent),
				Size:       int64(len(initrdContent)),
				Provenance: provenance,
			},
			Content: initrdContent,
		},
	}
}

func testPublicKey() string {
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32)) + " aegispxe-test"
}
