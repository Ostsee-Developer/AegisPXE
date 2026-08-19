package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/artifact"
	"github.com/Ostsee-Developer/AegisPXE/internal/assignment"
	"github.com/Ostsee-Developer/AegisPXE/internal/boot"
	"github.com/Ostsee-Developer/AegisPXE/internal/drivers/debian13"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/installation"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
	"github.com/Ostsee-Developer/AegisPXE/internal/secureboot"
	"github.com/Ostsee-Developer/AegisPXE/internal/trust"
)

type publicBootContext struct {
	Machine    machine.Machine
	Assignment assignment.Assignment
	Spec       installation.Spec
}

func (s *Server) registerProvisioning(mux *http.ServeMux) {
	s.registerProvisioningWithLoader(mux, artifact.NewHTTPLoader(s.logger))
}

func (s *Server) registerProvisioningWithLoader(mux *http.ServeMux, loader artifact.Loader) {
	mux.HandleFunc("GET /boot/installations/{id}/boot.ipxe", s.installationBootScript)
	mux.HandleFunc("GET /boot/installations/{id}/preseed.cfg", s.installationPreseed)
	mux.HandleFunc("GET /boot/installations/{id}/artifacts/{name}", func(w http.ResponseWriter, r *http.Request) {
		s.installationArtifact(w, r, loader)
	})
}

func (s *Server) assignmentDecision(ctx context.Context, item machine.Machine, requestID string) (boot.Decision, string, string, error) {
	base := boot.Decide(item.Policy)
	if item.Policy != machine.PolicyProvision {
		return base, "", "", nil
	}
	active, err := s.state.ActiveAssignmentForMachine(ctx, item.ID)
	if err != nil {
		if fault.Code(err) == fault.InstallationAssignmentNotFound {
			return base, "", "", nil
		}
		return boot.Decision{}, "", "", err
	}
	if !s.secureBootPolicy.AllowsProvision(item.SecureBootState) {
		s.logger.WarnContext(ctx, "Secure Boot policy blocked armed provisioning",
			"component", "boot.secureboot",
			"operation", "evaluate",
			"request_id", requestID,
			"machine_id", item.ID,
			"installation_id", active.InstallationID,
			"assignment_id", active.ID,
			"secure_boot_policy", s.secureBootPolicy,
			"secure_boot_state", item.SecureBootState,
			"firmware", item.Firmware,
			"error_code", fault.SecureBootRequired,
			"result", "rejected",
			"cause", "secure_boot_not_enabled",
		)
		return boot.Decision{Action: boot.ActionLocal, Reason: "secure_boot_required"}, "", "", nil
	}
	spec, err := s.state.InstallationSpec(ctx, active.InstallationID)
	if err != nil {
		return boot.Decision{}, "", "", err
	}
	if spec.MachineID != item.ID || active.MachineID != item.ID || active.State != assignment.StateArmed {
		s.logger.ErrorContext(ctx, "armed assignment is inconsistent with machine",
			"component", "boot.assignment",
			"operation", "evaluate",
			"request_id", requestID,
			"machine_id", item.ID,
			"installation_id", active.InstallationID,
			"assignment_id", active.ID,
			"error_code", fault.InstallationAssignmentInvalid,
			"result", "rejected",
			"cause", "assignment_machine_or_state_mismatch",
		)
		return boot.Decision{}, "", "", fault.New(fault.InstallationAssignmentInvalid, "armed installation assignment is inconsistent", nil)
	}
	gate := trust.Evaluate(item.Policy, true, false)
	if !gate.PublicBootAllowed {
		return boot.Decision{Action: boot.ActionLocal, Reason: gate.Reason}, "", "", nil
	}
	return boot.Decision{Action: boot.ActionProvision, Reason: "installation_armed"}, spec.ID, active.ID, nil
}

func (s *Server) installationBootScript(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	material, err := s.loadPublicBootContext(r.Context(), r.PathValue("id"), requestID(r.Context()), "serve_boot_script")
	if err != nil {
		s.writeBootMaterialError(w, r, err)
		return
	}
	bootSpec, err := debian13.RenderBoot(material.Spec)
	if err != nil {
		s.logger.WarnContext(r.Context(), "installation boot rendering rejected",
			"component", "httpapi.provisioning",
			"operation", "serve_boot_script",
			"request_id", requestID(r.Context()),
			"machine_id", material.Machine.ID,
			"installation_id", material.Spec.ID,
			"assignment_id", material.Assignment.ID,
			"driver_id", material.Spec.DriverID,
			"error_code", fault.DriverRenderFailed,
			"result", "rejected",
			"cause", err.Error(),
			"duration_ms", time.Since(started).Milliseconds(),
		)
		writeAPIError(w, http.StatusConflict, fault.DriverRenderFailed, "installation boot specification could not be rendered")
		return
	}
	args, err := renderIPXEKernelArguments(bootSpec.Arguments)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "rendered boot arguments could not be represented safely",
			"component", "httpapi.provisioning",
			"operation", "serve_boot_script",
			"request_id", requestID(r.Context()),
			"machine_id", material.Machine.ID,
			"installation_id", material.Spec.ID,
			"assignment_id", material.Assignment.ID,
			"error_code", fault.DriverRenderFailed,
			"result", "rejected",
			"cause", err.Error(),
			"duration_ms", time.Since(started).Milliseconds(),
		)
		writeAPIError(w, http.StatusConflict, fault.DriverRenderFailed, "installation boot arguments are unsafe")
		return
	}
	shim, ok := installationArtifactByName(material.Spec.Artifacts, "bootnetx64.efi")
	if !ok {
		s.logger.ErrorContext(r.Context(), "Secure Boot shim is absent from installation spec",
			"component", "boot.secureboot",
			"operation", "serve_boot_script",
			"request_id", requestID(r.Context()),
			"machine_id", material.Machine.ID,
			"installation_id", material.Spec.ID,
			"assignment_id", material.Assignment.ID,
			"error_code", fault.SecureBootAssetsInvalid,
			"result", "rejected",
		)
		writeAPIError(w, http.StatusConflict, fault.SecureBootAssetsInvalid, "signed Debian Secure Boot shim is missing")
		return
	}

	base := requestBaseURL(r)
	installationID := url.PathEscape(material.Spec.ID)
	kernelURL := base + "/boot/installations/" + installationID + "/artifacts/linux"
	initrdURL := base + "/boot/installations/" + installationID + "/artifacts/initrd.gz"
	shimURL := base + "/boot/installations/" + installationID + "/artifacts/bootnetx64.efi"
	preseedURL := base + "/boot/installations/" + installationID + "/preseed.cfg"

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintln(w, "#!ipxe")
	_, _ = fmt.Fprintf(w, "echo AegisPXE Debian 13 Secure Boot installation %s\n", ipxeSafe(material.Spec.ID))
	_, _ = fmt.Fprintln(w, "imgfree")
	_, _ = fmt.Fprintf(w, "kernel %s%s || goto boot_failed\n", kernelURL, args)
	_, _ = fmt.Fprintf(w, "initrd %s || goto boot_failed\n", initrdURL)
	_, _ = fmt.Fprintf(w, "initrd %s /preseed.cfg || goto boot_failed\n", preseedURL)
	_, _ = fmt.Fprintf(w, "shim %s || goto secure_boot_failed\n", shimURL)
	_, _ = fmt.Fprintln(w, "boot || goto boot_failed")
	_, _ = fmt.Fprintln(w, ":secure_boot_failed")
	_, _ = fmt.Fprintln(w, "echo AegisPXE refused installer boot because Debian shim validation failed")
	_, _ = fmt.Fprintln(w, "exit 1")
	_, _ = fmt.Fprintln(w, ":boot_failed")
	_, _ = fmt.Fprintln(w, "echo AegisPXE installer boot failed safely")
	_, _ = fmt.Fprintln(w, "exit 1")

	s.logger.InfoContext(r.Context(), "Secure Boot assignment-authorized installation boot script served",
		"component", "httpapi.provisioning",
		"operation", "serve_boot_script",
		"request_id", requestID(r.Context()),
		"machine_id", material.Machine.ID,
		"installation_id", material.Spec.ID,
		"assignment_id", material.Assignment.ID,
		"driver_id", material.Spec.DriverID,
		"driver_version", material.Spec.DriverVersion,
		"secure_boot_policy", s.secureBootPolicy,
		"secure_boot_state", material.Machine.SecureBootState,
		"shim_digest", shim.Digest,
		"result", "success",
		"duration_ms", time.Since(started).Milliseconds(),
	)
}

func (s *Server) installationPreseed(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	material, err := s.loadPublicBootContext(r.Context(), r.PathValue("id"), requestID(r.Context()), "serve_preseed")
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
		s.logger.WarnContext(r.Context(), "installation preseed could not be served",
			"component", "httpapi.provisioning",
			"operation", "serve_preseed",
			"request_id", requestID(r.Context()),
			"machine_id", material.Machine.ID,
			"installation_id", material.Spec.ID,
			"assignment_id", material.Assignment.ID,
			"driver_id", material.Spec.DriverID,
			"error_code", code,
			"result", "render_failed",
			"duration_ms", time.Since(started).Milliseconds(),
		)
		writeAPIError(w, http.StatusConflict, code, "installation preseed could not be rendered")
		return
	}

	consumed, err := s.state.ConsumeAssignment(r.Context(), material.Spec.ID, requestID(r.Context()), "system:pxe")
	if err != nil {
		code := fault.Code(err)
		if code == "" {
			code = fault.StorageFailure
		}
		s.logger.ErrorContext(r.Context(), "installation boot handoff could not be consumed",
			"component", "httpapi.provisioning",
			"operation", "consume_boot_handoff",
			"request_id", requestID(r.Context()),
			"machine_id", material.Machine.ID,
			"installation_id", material.Spec.ID,
			"assignment_id", material.Assignment.ID,
			"error_code", code,
			"result", "failure",
			"cause", err.Error(),
			"duration_ms", time.Since(started).Milliseconds(),
		)
		writeAPIError(w, http.StatusConflict, code, "installation boot handoff could not be committed")
		return
	}

	w.Header().Set("Content-Type", bundle.MediaType)
	w.Header().Set("Content-Length", strconv.Itoa(len(bundle.Content)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(bundle.Content); err != nil {
		s.logger.WarnContext(r.Context(), "installation preseed response write failed after one-shot handoff consumption",
			"component", "httpapi.provisioning",
			"operation", "serve_preseed",
			"request_id", requestID(r.Context()),
			"machine_id", material.Machine.ID,
			"installation_id", material.Spec.ID,
			"assignment_id", consumed.ID,
			"error_code", fault.DriverRenderFailed,
			"result", "response_write_failed",
			"duration_ms", time.Since(started).Milliseconds(),
		)
		return
	}
	s.logger.InfoContext(r.Context(), "one-shot assignment handoff committed and installation preseed served",
		"component", "httpapi.provisioning",
		"operation", "serve_preseed",
		"request_id", requestID(r.Context()),
		"machine_id", material.Machine.ID,
		"installation_id", material.Spec.ID,
		"assignment_id", consumed.ID,
		"assignment_state", consumed.State,
		"seed_bytes", len(bundle.Content),
		"secure_boot_policy", s.secureBootPolicy,
		"secure_boot_state", material.Machine.SecureBootState,
		"result", "success",
		"duration_ms", time.Since(started).Milliseconds(),
	)
}

func (s *Server) installationArtifact(w http.ResponseWriter, r *http.Request, loader artifact.Loader) {
	started := time.Now()
	material, err := s.loadPublicBootContext(r.Context(), r.PathValue("id"), requestID(r.Context()), "serve_artifact")
	if err != nil {
		s.writeBootMaterialError(w, r, err)
		return
	}
	if err := debian13.ValidateSpec(material.Spec); err != nil {
		s.logger.WarnContext(r.Context(), "installation artifact request rejected by driver",
			"component", "httpapi.provisioning",
			"operation", "serve_artifact",
			"request_id", requestID(r.Context()),
			"machine_id", material.Machine.ID,
			"installation_id", material.Spec.ID,
			"assignment_id", material.Assignment.ID,
			"driver_id", material.Spec.DriverID,
			"error_code", fault.DriverSpecUnsupported,
			"result", "driver_rejected",
			"cause", err.Error(),
			"duration_ms", time.Since(started).Milliseconds(),
		)
		writeAPIError(w, http.StatusConflict, fault.DriverSpecUnsupported, "installation is not bootable by the pinned driver")
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name != "linux" && name != "initrd.gz" && name != "bootnetx64.efi" {
		s.logger.WarnContext(r.Context(), "installation artifact request rejected",
			"component", "httpapi.provisioning",
			"operation", "serve_artifact",
			"request_id", requestID(r.Context()),
			"machine_id", material.Machine.ID,
			"installation_id", material.Spec.ID,
			"assignment_id", material.Assignment.ID,
			"artifact_name", name,
			"error_code", fault.ArtifactFetchFailed,
			"result", "invalid_artifact_role",
			"duration_ms", time.Since(started).Milliseconds(),
		)
		writeAPIError(w, http.StatusBadRequest, fault.ArtifactFetchFailed, "invalid boot artifact role")
		return
	}
	descriptor, ok := installationArtifactByName(material.Spec.Artifacts, name)
	if !ok {
		writeAPIError(w, http.StatusConflict, fault.DriverRenderFailed, "installation boot artifact is missing")
		return
	}
	content, err := loader.Load(r.Context(), descriptor, requestID(r.Context()), material.Spec.ID)
	if err != nil {
		code := fault.Code(err)
		if code == "" {
			code = fault.ArtifactFetchFailed
		}
		s.logger.WarnContext(r.Context(), "installation artifact could not be served",
			"component", "httpapi.provisioning",
			"operation", "serve_artifact",
			"request_id", requestID(r.Context()),
			"machine_id", material.Machine.ID,
			"installation_id", material.Spec.ID,
			"assignment_id", material.Assignment.ID,
			"artifact_id", descriptor.ID,
			"artifact_name", descriptor.Name,
			"artifact_digest", descriptor.Digest,
			"error_code", code,
			"result", "artifact_unavailable",
			"duration_ms", time.Since(started).Milliseconds(),
		)
		writeAPIError(w, http.StatusBadGateway, code, "verified boot artifact unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(content); err != nil {
		s.logger.WarnContext(r.Context(), "installation artifact response write failed",
			"component", "httpapi.provisioning",
			"operation", "serve_artifact",
			"request_id", requestID(r.Context()),
			"machine_id", material.Machine.ID,
			"installation_id", material.Spec.ID,
			"assignment_id", material.Assignment.ID,
			"artifact_id", descriptor.ID,
			"artifact_name", descriptor.Name,
			"error_code", fault.ArtifactFetchFailed,
			"result", "response_write_failed",
			"duration_ms", time.Since(started).Milliseconds(),
		)
		return
	}
	s.logger.InfoContext(r.Context(), "assignment-authorized verified boot artifact served",
		"component", "httpapi.provisioning",
		"operation", "serve_artifact",
		"request_id", requestID(r.Context()),
		"machine_id", material.Machine.ID,
		"installation_id", material.Spec.ID,
		"assignment_id", material.Assignment.ID,
		"artifact_id", descriptor.ID,
		"artifact_name", descriptor.Name,
		"artifact_digest", descriptor.Digest,
		"artifact_bytes", len(content),
		"secure_boot_policy", s.secureBootPolicy,
		"secure_boot_state", material.Machine.SecureBootState,
		"result", "success",
		"duration_ms", time.Since(started).Milliseconds(),
	)
}

func (s *Server) loadPublicBootContext(ctx context.Context, installationID, requestID, operation string) (publicBootContext, error) {
	installationID = strings.TrimSpace(installationID)
	if installationID == "" {
		s.logBootMaterialRejected(ctx, operation, requestID, "", "", "", fault.InstallationAssignmentInvalid, "missing_installation_id")
		return publicBootContext{}, fault.New(fault.InstallationAssignmentInvalid, "installation identifier is required", nil)
	}
	spec, err := s.state.InstallationSpec(ctx, installationID)
	if err != nil {
		s.logBootMaterialRejected(ctx, operation, requestID, "", installationID, "", fault.Code(err), "installation_lookup_failed")
		return publicBootContext{}, err
	}
	item, err := s.state.Machine(ctx, spec.MachineID)
	if err != nil {
		s.logBootMaterialRejected(ctx, operation, requestID, spec.MachineID, installationID, "", fault.Code(err), "machine_lookup_failed")
		return publicBootContext{}, err
	}
	active, err := s.state.AssignmentForInstallation(ctx, installationID)
	if err != nil {
		s.logBootMaterialRejected(ctx, operation, requestID, item.ID, installationID, "", fault.Code(err), "assignment_lookup_failed")
		return publicBootContext{}, err
	}
	if active.MachineID != item.ID || active.InstallationID != spec.ID {
		s.logBootMaterialRejected(ctx, operation, requestID, item.ID, installationID, active.ID, fault.InstallationAssignmentInvalid, "assignment_binding_mismatch")
		return publicBootContext{}, fault.New(fault.InstallationAssignmentInvalid, "installation assignment binding is invalid", nil)
	}
	if !s.secureBootPolicy.AllowsProvision(item.SecureBootState) {
		s.logBootMaterialRejected(ctx, operation, requestID, item.ID, installationID, active.ID, fault.SecureBootRequired, "secure_boot_not_enabled")
		s.logger.WarnContext(ctx, "Secure Boot policy rejected public installation material",
			"component", "boot.secureboot",
			"operation", operation,
			"request_id", requestID,
			"machine_id", item.ID,
			"installation_id", installationID,
			"assignment_id", active.ID,
			"secure_boot_policy", s.secureBootPolicy,
			"secure_boot_state", item.SecureBootState,
			"error_code", fault.SecureBootRequired,
			"result", "rejected",
		)
		return publicBootContext{}, fault.New(fault.SecureBootRequired, "UEFI Secure Boot is required before installation material may be served", nil)
	}
	gate := trust.Evaluate(item.Policy, active.State == assignment.StateArmed, false)
	if !gate.PublicBootAllowed {
		s.logBootMaterialRejected(ctx, operation, requestID, item.ID, installationID, active.ID, fault.InstallationAssignmentInvalid, gate.Reason)
		return publicBootContext{}, fault.New(fault.InstallationAssignmentInvalid, "installation public boot material is not authorized", nil)
	}
	return publicBootContext{Machine: item, Assignment: active, Spec: spec}, nil
}

func (s *Server) logBootMaterialRejected(ctx context.Context, operation, requestID, machineID, installationID, assignmentID, code, cause string) {
	if code == "" {
		code = fault.StorageFailure
	}
	s.logger.WarnContext(ctx, "installation public boot material request rejected",
		"component", "httpapi.provisioning",
		"operation", operation,
		"request_id", requestID,
		"machine_id", machineID,
		"installation_id", installationID,
		"assignment_id", assignmentID,
		"error_code", code,
		"result", "rejected",
		"cause", cause,
	)
}

func (s *Server) writeBootMaterialError(w http.ResponseWriter, _ *http.Request, err error) {
	code := fault.Code(err)
	status := http.StatusConflict
	switch code {
	case fault.InstallationNotFound, fault.InstallationAssignmentNotFound, fault.MachineNotFound:
		status = http.StatusNotFound
	case fault.SecureBootRequired:
		status = http.StatusForbidden
	case fault.StorageFailure:
		status = http.StatusServiceUnavailable
	}
	if code == "" {
		code = fault.StorageFailure
		status = http.StatusServiceUnavailable
	}
	writeAPIError(w, status, code, "installation boot material unavailable")
}

func (s *Server) writeProvisioningChain(w http.ResponseWriter, r *http.Request, installationID string) {
	endpoint := requestBaseURL(r) + "/boot/installations/" + url.PathEscape(installationID) + "/boot.ipxe"
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintln(w, "#!ipxe")
	_, _ = fmt.Fprintln(w, "echo AegisPXE Secure Boot provisioning assignment armed")
	_, _ = fmt.Fprintf(w, "chain %s || goto provisioning_failed\n", endpoint)
	_, _ = fmt.Fprintln(w, "exit 0")
	_, _ = fmt.Fprintln(w, ":provisioning_failed")
	_, _ = fmt.Fprintln(w, "echo AegisPXE provisioning endpoint unavailable; exiting safely")
	_, _ = fmt.Fprintln(w, "exit 1")
}

func renderIPXEKernelArguments(args []boot.Argument) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		if !ipxeArgumentSafe(arg.Key) || !ipxeArgumentSafe(arg.Value) {
			return "", fmt.Errorf("kernel argument %q contains unsupported iPXE characters", arg.Key)
		}
		if arg.Value == "" {
			parts = append(parts, arg.Key)
		} else {
			parts = append(parts, arg.Key+"="+arg.Value)
		}
	}
	return " " + strings.Join(parts, " "), nil
}

func ipxeArgumentSafe(value string) bool {
	if value == "" {
		return true
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case strings.ContainsRune("._/:,+-", r):
		default:
			return false
		}
	}
	return true
}

func installationArtifactByName(items []installation.Artifact, name string) (installation.Artifact, bool) {
	for _, item := range items {
		if item.Name == name {
			return item, true
		}
	}
	return installation.Artifact{}, false
}

var _ = secureboot.StateEnabled
