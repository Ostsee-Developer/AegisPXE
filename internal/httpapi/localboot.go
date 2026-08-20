package httpapi

import (
	"fmt"
	"net/http"
	"strings"
)

func localBootReason(reason string) bool {
	switch strings.TrimSpace(reason) {
	case "local_policy", "installation_not_armed", "pending_approval", "secure_boot_required":
		return true
	default:
		return false
	}
}

func (s *Server) writeLocalBootIPXE(w http.ResponseWriter, message, reason string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintln(w, "#!ipxe")
	_, _ = fmt.Fprintf(w, "echo %s\n", ipxeSafe(message))
	_, _ = fmt.Fprintf(w, "echo Decision: %s\n", ipxeSafe(reason))
	_, _ = fmt.Fprintln(w, "iseq ${platform} efi && goto local_efi || goto local_bios")
	_, _ = fmt.Fprintln(w, ":local_efi")
	_, _ = fmt.Fprintln(w, "echo AegisPXE searching local UEFI bootloaders")
	_, _ = fmt.Fprintln(w, `sanboot --no-describe --drive 0 --filename \EFI\debian\shimx64.efi || goto local_efi_grub`)
	_, _ = fmt.Fprintln(w, ":local_efi_grub")
	_, _ = fmt.Fprintln(w, `sanboot --no-describe --drive 0 --filename \EFI\debian\grubx64.efi || goto local_efi_fallback`)
	_, _ = fmt.Fprintln(w, ":local_efi_fallback")
	_, _ = fmt.Fprintln(w, `sanboot --no-describe --drive 0 --filename \EFI\BOOT\BOOTX64.EFI || goto local_efi_any`)
	_, _ = fmt.Fprintln(w, ":local_efi_any")
	_, _ = fmt.Fprintln(w, "sanboot --no-describe --drive 0 || goto local_firmware")
	_, _ = fmt.Fprintln(w, ":local_bios")
	_, _ = fmt.Fprintln(w, "echo AegisPXE booting first local BIOS disk")
	_, _ = fmt.Fprintln(w, "sanboot --no-describe --drive 0x80 || goto local_firmware")
	_, _ = fmt.Fprintln(w, ":local_firmware")
	_, _ = fmt.Fprintln(w, "echo AegisPXE local bootloader not found; returning failure to firmware")
	_, _ = fmt.Fprintln(w, "exit 1")
}
