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
)

const (
	reporterAMD64Path    = "/usr/lib/aegispxe/reporters/aegispxe-reporter-amd64"
	maxReporterBootBytes = 32 << 20
)

func (s *Server) registerReporterBoot(mux *http.ServeMux) {
	mux.HandleFunc("GET /boot/installations/{id}/boot.ipxe", s.installationBootScriptWithReporter)
	mux.HandleFunc("GET /boot/installations/{id}/reporter", s.installationReporterBinary)
	mux.HandleFunc("GET /boot/installations/{id}/reporter.json", s.installationReporterConfig)
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
	reporterURL := prefix + "/reporter"
	reporterConfigURL := prefix + "/reporter.json"
	preseedURL := prefix + "/preseed.cfg"

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintln(w, "#!ipxe")
	_, _ = fmt.Fprintf(w, "echo AegisPXE Debian 13 installation %s with TPM reporter\n", ipxeSafe(material.Spec.ID))
	_, _ = fmt.Fprintln(w, "imgfree")
	_, _ = fmt.Fprintf(w, "kernel %s%s || goto boot_failed\n", kernelURL, args)
	_, _ = fmt.Fprintf(w, "initrd %s || goto boot_failed\n", initrdURL)
	_, _ = fmt.Fprintf(w, "initrd %s /aegispxe/reporter || goto boot_failed\n", reporterURL)
	_, _ = fmt.Fprintf(w, "initrd %s /aegispxe/reporter.json || goto boot_failed\n", reporterConfigURL)
	// Preseed deliberately remains the final network object. Its successful
	// response commits the one-shot destructive boot handoff.
	_, _ = fmt.Fprintf(w, "initrd %s /preseed.cfg || goto boot_failed\n", preseedURL)
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
	info, err := os.Stat(reporterAMD64Path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxReporterBootBytes {
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
	file, err := os.Open(reporterAMD64Path)
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, fault.DriverRenderFailed, "Debian reporter binary unavailable")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, ""); err != nil {
		return
	}
	if _, err := file.WriteTo(w); err != nil {
		s.logger.WarnContext(r.Context(), "Debian reporter binary response write failed", "component", "httpapi.provisioning", "operation", "serve_reporter_binary", "request_id", requestID(r.Context()), "machine_id", material.Machine.ID, "installation_id", material.Spec.ID, "error_code", fault.DriverRenderFailed, "result", "response_write_failed")
		return
	}
	s.logger.InfoContext(r.Context(), "Debian reporter binary served", "component", "httpapi.provisioning", "operation", "serve_reporter_binary", "request_id", requestID(r.Context()), "machine_id", material.Machine.ID, "installation_id", material.Spec.ID, "reporter_bytes", info.Size(), "result", "success", "duration_ms", time.Since(started).Milliseconds())
}

func (s *Server) installationReporterConfig(w http.ResponseWriter, r *http.Request) {
	material, err := s.loadPublicBootContext(r.Context(), r.PathValue("id"), requestID(r.Context()), "serve_reporter_config")
	if err != nil {
		s.writeBootMaterialError(w, r, err)
		return
	}
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
	content, err := json.Marshal(payload)
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
