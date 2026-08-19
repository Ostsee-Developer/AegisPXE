package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBootTrustHandlerKeepsKnownGoodDebianBootTransport(t *testing.T) {
	state, _ := testServer(t)
	_, spec := createArmedProvisioningState(t, state, "52:54:00:60:00:01")
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := New(state, logger, "test").HandlerWithBootTrust()

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
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("known-good Debian boot script missing %q: %s", want, body)
		}
	}
	for _, forbidden := range []string{"reporter", "overlay.cpio", "initrd.img", "initrd=", "--name", "initrd.magic"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("known-good Debian boot script contains suspended transport fragment %q: %s", forbidden, body)
		}
	}
}

func TestKnownGoodDebianPreseedDoesNotDependOnReporterRuntime(t *testing.T) {
	state, _ := testServer(t)
	_, spec := createArmedProvisioningState(t, state, "52:54:00:60:00:02")
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := New(state, logger, "test").HandlerWithBootTrust()

	req := httptest.NewRequest(http.MethodGet, "http://aegispxe.test/boot/installations/"+spec.ID+"/preseed.cfg", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("preseed status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := strings.ToLower(rec.Body.String())
	for _, forbidden := range []string{"/aegispxe/reporter", "installer_started", "install-firstboot", "reporter.json"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("known-good Preseed still depends on suspended reporter runtime: %q", forbidden)
		}
	}
}
