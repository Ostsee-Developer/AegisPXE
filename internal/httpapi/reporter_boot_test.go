package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/assignment"
)

func TestReporterBootScriptUsesExplicitNamedInitrdsAsFinalNetworkObject(t *testing.T) {
	state, _ := testServer(t)
	machineRecord, spec := createArmedProvisioningState(t, state, "52:54:00:40:00:72")
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := New(state, logger, "test").HandlerWithBootTrust()

	req := httptest.NewRequest(http.MethodGet, "http://aegispxe.test/boot/installations/"+spec.ID+"/boot.ipxe", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reporter boot script status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	kernelLine := "kernel http://aegispxe.test/boot/installations/" + spec.ID + "/artifacts/linux initrd=initrd.gz initrd=overlay.cpio"
	baseInitrdLine := "initrd --name initrd.gz http://aegispxe.test/boot/installations/" + spec.ID + "/artifacts/initrd.gz"
	overlayLine := "initrd --name overlay.cpio http://aegispxe.test/boot/installations/" + spec.ID + "/overlay.cpio"
	for _, want := range []string{kernelLine, baseInitrdLine, overlayLine, "imgstat"} {
		if !strings.Contains(body, want) {
			t.Fatalf("reporter boot script missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "initrd=initrd.magic") {
		t.Fatalf("UEFI boot script still depends on initrd.magic aggregation: %s", body)
	}
	if strings.Contains(body, "/aegispxe/reporter mode=") || strings.Contains(body, "/reporter.json /aegispxe/") || strings.Contains(body, "/preseed.cfg /preseed.cfg") {
		t.Fatalf("boot script still depends on iPXE per-file magic-initrd injection: %s", body)
	}
	if strings.Index(body, baseInitrdLine) > strings.Index(body, overlayLine) {
		t.Fatalf("native Debian initrd must load before final overlay: %s", body)
	}
	if strings.Contains(body, spec.LifecycleCredentialID) || strings.Contains(strings.ToLower(body), "authorization") || strings.Contains(strings.ToLower(body), "token=") {
		t.Fatal("reporter-enabled boot script leaked lifecycle authentication material")
	}
	current, err := state.ActiveAssignmentForMachine(context.Background(), machineRecord.ID)
	if err != nil || current.State != assignment.StateArmed {
		t.Fatalf("boot script read changed assignment: %+v err=%v", current, err)
	}
}

func TestReporterConfigContainsOnlyNonSecretInstallationIdentity(t *testing.T) {
	state, _ := testServer(t)
	machineRecord, spec := createArmedProvisioningState(t, state, "52:54:00:40:00:73")
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := New(state, logger, "test").HandlerWithBootTrust()

	req := httptest.NewRequest(http.MethodGet, "http://aegispxe.test/boot/installations/"+spec.ID+"/reporter.json", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reporter config status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{spec.ID, machineRecord.ID, spec.Profile.Admin.Username, "http://aegispxe.test"} {
		if !strings.Contains(body, want) {
			t.Fatalf("reporter config missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, spec.LifecycleCredentialID) || strings.Contains(strings.ToLower(body), "secret") || strings.Contains(strings.ToLower(body), "bearer") {
		t.Fatalf("reporter config contains secret-bearing material: %s", body)
	}
}
