package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
	"github.com/Ostsee-Developer/AegisPXE/internal/observability"
	"github.com/Ostsee-Developer/AegisPXE/internal/secureboot"
	"github.com/Ostsee-Developer/AegisPXE/internal/store"
)

func TestRequiredSecureBootPolicyRejectsDisabledMachineBeforeProvisioning(t *testing.T) {
	var logs bytes.Buffer
	logger := observability.New(&logs, slog.LevelDebug)
	state, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "aegispxe.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()

	item, _, err := state.DiscoverMachine(context.Background(), machine.Observation{
		MAC: "52:54:00:61:00:01", Architecture: "x86_64", Firmware: "efi", SecureBoot: "00", SetupMode: "00",
	}, "req_sb_disabled_discover")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.SetMachinePolicy(context.Background(), item.ID, machine.PolicyProvision, "req_sb_disabled_approve", "test:operator"); err != nil {
		t.Fatal(err)
	}
	spec := createProvisioningSpec(t, state, item.ID, "req_sb_disabled_spec")
	if _, err := state.ArmInstallation(context.Background(), item.ID, spec.ID, "req_sb_disabled_arm", "test:operator"); err != nil {
		t.Fatal(err)
	}

	server := NewWithConfig(state, logger, "test", Config{SecureBootPolicy: secureboot.PolicyRequired, SecureBootAssetsValid: true})
	form := url.Values{
		"mac":          {"52:54:00:61:00:01"},
		"architecture": {"x86_64"},
		"firmware":     {"efi"},
		"secure_boot":  {"00"},
		"setup_mode":   {"00"},
	}
	req := httptest.NewRequest(http.MethodGet, "http://aegispxe.test/api/v1/discovery.ipxe?"+form.Encode(), nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("discovery status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Decision: secure_boot_required") || strings.Contains(body, "/boot/installations/") {
		t.Fatalf("required Secure Boot did not fail closed: %s", body)
	}
	if !strings.Contains(logs.String(), fault.SecureBootRequired) || !strings.Contains(logs.String(), `"secure_boot_state":"disabled"`) {
		t.Fatalf("Secure Boot rejection was not logged with stable evidence: %s", logs.String())
	}
}

func TestRequiredSecureBootPolicyProtectsDirectBootMaterial(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	state, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "aegispxe.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()

	item, _, err := state.DiscoverMachine(context.Background(), machine.Observation{
		MAC: "52:54:00:61:00:02", Architecture: "x86_64", Firmware: "efi", SecureBoot: "00", SetupMode: "00",
	}, "req_sb_direct_discover")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.SetMachinePolicy(context.Background(), item.ID, machine.PolicyProvision, "req_sb_direct_approve", "test:operator"); err != nil {
		t.Fatal(err)
	}
	spec := createProvisioningSpec(t, state, item.ID, "req_sb_direct_spec")
	if _, err := state.ArmInstallation(context.Background(), item.ID, spec.ID, "req_sb_direct_arm", "test:operator"); err != nil {
		t.Fatal(err)
	}

	server := NewWithConfig(state, logger, "test", Config{SecureBootPolicy: secureboot.PolicyRequired, SecureBootAssetsValid: true})
	req := httptest.NewRequest(http.MethodGet, "http://aegispxe.test/boot/installations/"+spec.ID+"/boot.ipxe", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), fault.SecureBootRequired) {
		t.Fatalf("direct boot material bypassed Secure Boot policy: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRequiredSecureBootEnabledMachineGetsSignedDebianShimBootScript(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	state, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "aegispxe.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	_, spec := createArmedProvisioningState(t, state, "52:54:00:61:00:03")

	server := NewWithConfig(state, logger, "test", Config{SecureBootPolicy: secureboot.PolicyRequired, SecureBootAssetsValid: true})
	req := httptest.NewRequest(http.MethodGet, "http://aegispxe.test/boot/installations/"+spec.ID+"/boot.ipxe", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("boot script status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/artifacts/bootnetx64.efi") || !strings.Contains(body, "shim http://") {
		t.Fatalf("Secure Boot shim was not configured: %s", body)
	}

	kernelIndex := strings.Index(body, "kernel http://")
	initrdIndex := strings.Index(body, "/artifacts/initrd.gz")
	shimIndex := strings.Index(body, "shim http://")
	preseedIndex := strings.Index(body, "/preseed.cfg")
	bootIndex := strings.Index(body, "\nboot || goto boot_failed")
	if kernelIndex < 0 || initrdIndex < 0 || shimIndex < 0 || preseedIndex < 0 || bootIndex < 0 ||
		!(kernelIndex < initrdIndex && initrdIndex < shimIndex && shimIndex < preseedIndex && preseedIndex < bootIndex) {
		t.Fatalf("Secure Boot material order must fetch shim before one-shot preseed handoff: %s", body)
	}
	if !strings.Contains(body, "Secure Boot shim fetch or configuration failed") || strings.Contains(body, "shim validation failed") {
		t.Fatalf("Secure Boot shim failure message is misleading: %s", body)
	}

	for _, forbidden := range []string{"reporter", "overlay.cpio", "initrd.img", "initrd=", "initrd.magic"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("Secure Boot reintroduced suspended installer transport %q: %s", forbidden, body)
		}
	}
}
