package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/artifact"
	"github.com/Ostsee-Developer/AegisPXE/internal/drivers/debian13"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
	"github.com/Ostsee-Developer/AegisPXE/internal/store"
)

func WithArtifactServing(next http.Handler, state *store.Store, logger *slog.Logger) http.Handler {
	return withArtifactServing(next, state, logger, artifact.NewHTTPLoader(logger))
}

func withArtifactServing(next http.Handler, state *store.Store, logger *slog.Logger, loader artifact.Loader) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /boot/installations/{id}/artifacts/{name}", func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		reqID, err := idgen.New("req_")
		if err != nil {
			logger.ErrorContext(r.Context(), "boot artifact request could not allocate correlation ID", "component", "httpapi.boot_artifact", "operation", "serve_verified_artifact", "error_code", fault.StorageFailure, "result", "failure")
			writeAPIError(w, http.StatusServiceUnavailable, fault.StorageFailure, "boot artifact service unavailable")
			return
		}
		ctx := context.WithValue(r.Context(), requestIDKey{}, reqID)
		r = r.WithContext(ctx)

		installationID := strings.TrimSpace(r.PathValue("id"))
		name := strings.TrimSpace(r.PathValue("name"))
		if installationID == "" || (name != "linux" && name != "initrd.gz") {
			logger.WarnContext(ctx, "boot artifact request rejected", "component", "httpapi.boot_artifact", "operation", "serve_verified_artifact", "request_id", reqID, "installation_id", installationID, "artifact_name", name, "error_code", fault.ArtifactFetchFailed, "result", "invalid_request", "duration_ms", time.Since(started).Milliseconds())
			writeAPIError(w, http.StatusBadRequest, fault.ArtifactFetchFailed, "invalid boot artifact request")
			return
		}

		spec, err := state.InstallationSpec(ctx, installationID)
		if err != nil {
			code := fault.Code(err)
			status := http.StatusInternalServerError
			message := "could not load installation"
			if code == fault.InstallationNotFound {
				status = http.StatusNotFound
				message = "installation not found"
			}
			logger.WarnContext(ctx, "boot artifact installation lookup failed", "component", "httpapi.boot_artifact", "operation", "serve_verified_artifact", "request_id", reqID, "installation_id", installationID, "artifact_name", name, "error_code", code, "result", "lookup_failed", "duration_ms", time.Since(started).Milliseconds())
			writeAPIError(w, status, code, message)
			return
		}
		if err := debian13.ValidateSpec(spec); err != nil {
			logger.WarnContext(ctx, "boot artifact request rejected by driver", "component", "httpapi.boot_artifact", "operation", "serve_verified_artifact", "request_id", reqID, "machine_id", spec.MachineID, "installation_id", spec.ID, "driver_id", spec.DriverID, "driver_version", spec.DriverVersion, "artifact_name", name, "error_code", fault.DriverSpecUnsupported, "result", "driver_rejected", "duration_ms", time.Since(started).Milliseconds())
			writeAPIError(w, http.StatusConflict, fault.DriverSpecUnsupported, "installation is not bootable by the pinned driver")
			return
		}

		descriptor, ok := installationArtifact(spec.Artifacts, name)
		if !ok {
			logger.ErrorContext(ctx, "pinned boot artifact missing after driver validation", "component", "httpapi.boot_artifact", "operation", "serve_verified_artifact", "request_id", reqID, "machine_id", spec.MachineID, "installation_id", spec.ID, "driver_id", spec.DriverID, "artifact_name", name, "error_code", fault.DriverRenderFailed, "result", "inconsistent_spec", "duration_ms", time.Since(started).Milliseconds())
			writeAPIError(w, http.StatusConflict, fault.DriverRenderFailed, "installation boot specification is inconsistent")
			return
		}

		content, err := loader.Load(ctx, descriptor, reqID, spec.ID)
		if err != nil {
			code := fault.Code(err)
			if code == "" {
				code = fault.ArtifactFetchFailed
			}
			logger.WarnContext(ctx, "boot artifact could not be served", "component", "httpapi.boot_artifact", "operation", "serve_verified_artifact", "request_id", reqID, "machine_id", spec.MachineID, "installation_id", spec.ID, "driver_id", spec.DriverID, "artifact_id", descriptor.ID, "artifact_name", descriptor.Name, "artifact_digest", descriptor.Digest, "error_code", code, "result", "artifact_unavailable", "duration_ms", time.Since(started).Milliseconds())
			writeAPIError(w, http.StatusBadGateway, code, "verified boot artifact unavailable")
			return
		}

		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(content); err != nil {
			logger.WarnContext(ctx, "boot artifact response write failed", "component", "httpapi.boot_artifact", "operation", "serve_verified_artifact", "request_id", reqID, "machine_id", spec.MachineID, "installation_id", spec.ID, "artifact_id", descriptor.ID, "artifact_name", descriptor.Name, "artifact_digest", descriptor.Digest, "error_code", fault.ArtifactFetchFailed, "result", "response_write_failed", "duration_ms", time.Since(started).Milliseconds())
			return
		}
		logger.InfoContext(ctx, "verified boot artifact served", "component", "httpapi.boot_artifact", "operation", "serve_verified_artifact", "request_id", reqID, "machine_id", spec.MachineID, "installation_id", spec.ID, "driver_id", spec.DriverID, "driver_version", spec.DriverVersion, "artifact_id", descriptor.ID, "artifact_name", descriptor.Name, "artifact_digest", descriptor.Digest, "artifact_bytes", len(content), "result", "success", "duration_ms", time.Since(started).Milliseconds())
	})
	mux.Handle("/", next)
	return mux
}

func installationArtifact(items []artifact.Descriptor, name string) (artifact.Descriptor, bool) {
	for _, item := range items {
		if item.Name == name {
			return item, true
		}
	}
	return artifact.Descriptor{}, false
}
