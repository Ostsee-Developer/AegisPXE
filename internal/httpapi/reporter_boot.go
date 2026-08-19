package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/drivers/debian13"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/initramfs"
)

const (
	reporterAMD64Path    = "/usr/lib/aegispxe/reporters/aegispxe-reporter-amd64"
	maxReporterBootBytes = 32 << 20
)

func (s *Server) registerReporterBoot(mux *http.ServeMux) {
	mux.HandleFunc("GET /boot/installations/{id}/boot.ipxe", s.installationBootScriptWithReporter)
	mux.HandleFunc("GET /boot/installations/{id}/reporter", s.installationReporterBinary)
	mux.HandleFunc("GET /boot/installations/{id}/reporter.json", s.installationReporterConfig)
	mux.HandleFunc("GET /boot/installations/{id}/overlay.cpio", s.installationReporterOverlay)
}

func (s *Server) installationBootScriptWithReporter(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	material, err := s.loadPublicBootContext(r.Context(), r.PathValue("id"), requestID(r.Context()), "serve_reporter_boot_script")
	if err != nil {
		s.writeBootMaterialError(w, r, err)
		return
	}
	bootSpec, err := debian13.RenderBoot(material.Spec)
	if err != nil {
		writeAPIError(w, http.StatusConflict, fault.DriverRenderFailed, "installation boot specification could not be rendered")
		return
	}
	args, err := renderIPXEKernelArguments(bootSpec.Arguments)
	if err != nil {
		writeAPIError(w, http.StatusConflict, fault.DriverRenderFailed, "installation boot arguments are unsafe")
		return
	}

	base := requestBaseURL(r)
	installationID := url.PathEscape(material.Spec.ID)
	prefix := base + "/boot/installations/" + installationID
	kernelURL := prefix + "/artifacts/linux"
	initrdURL := prefix + "/artifacts/initrd.gz"
	overlayURL := prefix + "/overlay.cpio"
	initrdArgument := ""
	if material.Machine.Firmware == "efi" {
		initrdArgument = " initrd=initrd.magic"
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintln(w, "#!ipxe")
	_, _ = fmt.Fprintf(w, "echo AegisPXE Debian 13 installation %s with TPM reporter\n", ipxeSafe(material.Spec.ID))
	_, _ = fmt.Fprintln(w, "imgfree")
	// Do not depend on iPXE's per-file magic-initrd injection support. Older
	// packaged iPXE builds can boot Debian's native initrd but cannot reliably
	// synthesize executable files/directories from initrd command arguments.
	// We therefore load two real initramfs archives. On UEFI, initrd.magic
	// explicitly exposes the aggregate to the Linux EFI stub.
	_, _ = fmt.Fprintf(w, "kernel %s%s%s || goto boot_failed\n", kernelURL, initrdArgument, args)
	_, _ = fmt.Fprintf(w, "initrd %s || goto boot_failed\n", initrdURL)
	// The overlay is deliberately the final network object. It contains the
	// reporter, non-secret reporter config and preseed.cfg, and its successful
	// serving commits the one-shot destructive handoff.
	_, _ = fmt.Fprintf(w, "initrd %s || goto boot_failed\n", overlayURL)
	_, _ = fmt.Fprintln(w, "boot || goto boot_failed")
	_, _ = fmt.Fprintln(w, ":boot_failed")
	_, _ = fmt.Fprintln(w, "echo AegisPXE installer boot failed safely")
	_, _ = fmt.Fprintln(w, "exit 1")

	s.logger.InfoContext(r.Context(), "reporter-enabled installation boot script served",
		"component", "httpapi.provisioning",
		"operation", "serve_reporter_boot_script",
		"request_id", requestID(r.Context()),
		"machine_id", material.Machine.ID,
		"installation_id", material.Spec.ID,
		"assignment_id", material.Assignment.ID,
		"driver_id", material.Spec.DriverID,
		"driver_version", material.Spec.DriverVersion,
		"initramfs_strategy", "native_plus_newc_overlay",
		"result", "success",
		"duration_ms", time.Since(started).Milliseconds(),
	)
}

func (s *Server) installationReporterOverlay(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	material, err := s.loadPublicBootContext(r.Context(), r.PathValue("id"), requestID(r.Context()), "serve_reporter_overlay")
	if err != nil {
		s.writeBootMaterialError(w, r, err)
		return
	}
	bundle, err := debian13.RenderSeed(r.Context(), s.logger, material.Spec, requestID(r.Context()))
	if err != nil {
		code := fault.Code(err)
		if code == "" {
			code = fault.DriverRenderFailed
		}
		writeAPIError(w, http.StatusConflict, code, "installation overlay could not render preseed")
		return
	}
	reporter, err := readReporterBinary()
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Debian reporter overlay unavailable",
			"component", "httpapi.provisioning",
			"operation", "serve_reporter_overlay",
			"request_id", requestID(r.Context()),
			"machine_id", material.Machine.ID,
			"installation_id", material.Spec.ID,
			"error_code", fault.DriverRenderFailed,
			"result", "failure",
			"cause", "reporter_binary_missing_or_invalid",
		)
		writeAPIError(w, http.StatusServiceUnavailable, fault.DriverRenderFailed, "Debian reporter overlay unavailable")
		return
	}
	config, err := reporterConfigJSON(r, material)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, fault.DriverRenderFailed, "reporter configuration could not be rendered")
		return
	}
	overlay, err := initramfs.BuildNewc([]initramfs.Entry{
		{Path: "aegispxe", Mode: initramfs.ModeDirectory | 0o755},
		{Path: "aegispxe/reporter", Mode: initramfs.ModeRegular | 0o755, Data: reporter},
		{Path: "aegispxe/reporter.json", Mode: initramfs.ModeRegular | 0o600, Data: config},
		{Path: "preseed.cfg", Mode: initramfs.ModeRegular | 0o600, Data: bundle.Content},
	})
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Debian reporter overlay build failed", "component", "httpapi.provisioning", "operation", "serve_reporter_overlay", "request_id", requestID(r.Context()), "machine_id", material.Machine.ID, "installation_id", material.Spec.ID, "error_code", fault.DriverRenderFailed, "result", "failure", "cause", err.Error())
		writeAPIError(w, http.StatusInternalServerError, fault.DriverRenderFailed, "Debian reporter overlay could not be built")
		return
	}

	consumed, err := s.state.ConsumeAssignment(r.Context(), material.Spec.ID, requestID(r.Context()), "system:pxe")
	if err != nil {
		code := fault.Code(err)
		if code == "" {
			code = fault.StorageFailure
		}
		s.logger.ErrorContext(r.Context(), "installation overlay handoff could not be consumed", "component", "httpapi.provisioning", "operation", "consume_reporter_overlay_handoff", "request_id", requestID(r.Context()), "machine_id", material.Machine.ID, "installation_id", material.Spec.ID, "assignment_id", material.Assignment.ID, "error_code", code, "result", "failure", "cause", err.Error())
		writeAPIError(w, http.StatusConflict, code, "installation boot handoff could not be committed")
		return
	}

	w.Header().Set("Content-Type", "application/x-cpio")
	w.Header().Set("Content-Length", strconv.Itoa(len(overlay)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(overlay); err != nil {
		s.logger.WarnContext(r.Context(), "installation reporter overlay response write failed after one-shot handoff consumption", "component", "httpapi.provisioning", "operation", "serve_reporter_overlay", "request_id", requestID(r.Context()), "machine_id", material.Machine.ID, "installation_id", material.Spec.ID, "assignment_id", consumed.ID, "error_code", fault.DriverRenderFailed, "result", "response_write_failed", "duration_ms", time.Since(started).Milliseconds())
		return
	}
	s.logger.InfoContext(r.Context(), "one-shot assignment handoff committed and reporter initramfs overlay served",
		"component", "httpapi.provisioning",
		"operation", "serve_reporter_overlay",
		"request_id", requestID(r.Context()),
		"machine_id", material.Machine.ID,
		"installation_id", material.Spec.ID,
		"assignment_id", consumed.ID,
		"assignment_state", consumed.State,
		"overlay_bytes", len(overlay),
		"reporter_bytes", len(reporter),
		"seed_bytes", len(bundle.Content),
		"result", "success",
		"duration_ms", time.Since(started).Milliseconds(),
	)
}

func (s *Server) installationReporterBinary(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	material, err := s.loadPublicBootContext(r.Context(), r.PathValue("id"), requestID(r.Context()), "serve_reporter_binary")
	if err != nil {
		s.writeBootMaterialError(w, r, err)
		return
	}
	reporter, err := readReporterBinary()
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Debian reporter binary unavailable",
			"component", "httpapi.provisioning",
			"operation", "serve_reporter_binary",
			"request_id", requestID(r.Context()),
			"machine_id", material.Machine.ID,
			"installation_id", material.Spec.ID,
			"error_code", fault.DriverRenderFailed,
			"result", "failure",
			"cause", "reporter_binary_missing_or_invalid",
		)
		writeAPIError(w, http.StatusServiceUnavailable, fault.DriverRenderFailed, "Debian reporter binary unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(reporter)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(reporter); err != nil {
		s.logger.WarnContext(r.Context(), "Debian reporter binary response write failed", "component", "httpapi.provisioning", "operation", "serve_reporter_binary", "request_id", requestID(r.Context()), "machine_id", material.Machine.ID, "installation_id", material.Spec.ID, "error_code", fault.DriverRenderFailed, "result", "response_write_failed")
		return
	}
	s.logger.InfoContext(r.Context(), "Debian reporter binary served", "component", "httpapi.provisioning", "operation", "serve_reporter_binary", "request_id", requestID(r.Context()), "machine_id", material.Machine.ID, "installation_id", material.Spec.ID, "reporter_bytes", len(reporter), "result", "success", "duration_ms", time.Since(started).Milliseconds())
}

func (s *Server) installationReporterConfig(w http.ResponseWriter, r *http.Request) {
	material, err := s.loadPublicBootContext(r.Context(), r.PathValue("id"), requestID(r.Context()), "serve_reporter_config")
	if err != nil {
		s.writeBootMaterialError(w, r, err)
		return
	}
	content, err := reporterConfigJSON(r, material)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, fault.DriverRenderFailed, "reporter configuration could not be rendered")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
	s.logger.InfoContext(r.Context(), "Debian reporter configuration served", "component", "httpapi.provisioning", "operation", "serve_reporter_config", "request_id", requestID(r.Context()), "machine_id", material.Machine.ID, "installation_id", material.Spec.ID, "result", "success")
}

func reporterConfigJSON(r *http.Request, material publicBootContext) ([]byte, error) {
	payload := struct {
		APIBase        string `json:"api_base"`
		InstallationID string `json:"installation_id"`
		MachineID      string `json:"machine_id"`
		AdminUsername  string `json:"admin_username"`
	}{
		APIBase:        requestBaseURL(r),
		InstallationID: material.Spec.ID,
		MachineID:      material.Machine.ID,
		AdminUsername:  material.Spec.Profile.Admin.Username,
	}
	return json.Marshal(payload)
}

func readReporterBinary() ([]byte, error) {
	info, err := os.Stat(reporterAMD64Path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxReporterBootBytes {
		return nil, fmt.Errorf("reporter binary is missing or invalid")
	}
	content, err := os.ReadFile(reporterAMD64Path)
	if err != nil {
		return nil, err
	}
	if int64(len(content)) != info.Size() {
		return nil, fmt.Errorf("reporter binary changed while reading")
	}
	return content, nil
}
