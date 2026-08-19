package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalBootReason(t *testing.T) {
	for _, reason := range []string{"local_policy", "installation_not_armed", "pending_approval"} {
		if !localBootReason(reason) {
			t.Fatalf("expected %q to trigger local boot", reason)
		}
	}
	for _, reason := range []string{"machine_blocked", "rate_limited", "server_rejected", ""} {
		if localBootReason(reason) {
			t.Fatalf("did not expect %q to trigger local boot", reason)
		}
	}
}

func TestWriteLocalBootIPXETrysUEFIAndBIOSLoaders(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&Server{}).writeLocalBootIPXE(recorder, "Machine ready for local boot", "installation_not_armed")
	body := recorder.Body.String()
	for _, want := range []string{
		"#!ipxe",
		"iseq ${platform} efi && goto local_efi || goto local_bios",
		`sanboot --no-describe --drive 0 --filename \EFI\debian\shimx64.efi`,
		`sanboot --no-describe --drive 0 --filename \EFI\debian\grubx64.efi`,
		`sanboot --no-describe --drive 0 --filename \EFI\BOOT\BOOTX64.EFI`,
		"sanboot --no-describe --drive 0 || goto local_firmware",
		"sanboot --no-describe --drive 0x80 || goto local_firmware",
		"exit 1",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("local boot script missing %q:\n%s", want, body)
		}
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("unexpected cache control: %q", got)
	}
}
