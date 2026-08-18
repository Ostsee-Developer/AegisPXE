package operatorui

import (
	"net/http"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/assignment"
	"github.com/Ostsee-Developer/AegisPXE/internal/drivers/debian13"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
	"github.com/Ostsee-Developer/AegisPXE/internal/installation"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
	"github.com/Ostsee-Developer/AegisPXE/internal/operator"
	"github.com/Ostsee-Developer/AegisPXE/internal/profile"
)

func (h *DashboardHandler) dashboardMachines(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireDashboardPage(w, r)
	if !ok {
		return
	}
	machines, err := h.state.Machines(r.Context())
	if err != nil {
		h.writeDashboardError(w, r, "machines", err)
		return
	}
	h.renderDashboard(w, dashboardView{Page: "machines", Title: "Machines", Description: "Discovered PXE clients and explicit provisioning policy.", Session: session, Machines: machines})
}

func (h *DashboardHandler) dashboardMachine(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireDashboardPage(w, r)
	if !ok {
		return
	}
	machineID := strings.TrimSpace(r.PathValue("id"))
	item, err := h.state.Machine(r.Context(), machineID)
	if err != nil {
		h.writeDashboardError(w, r, "machine", err)
		return
	}
	identifiers, err := h.state.MachineIdentifiers(r.Context(), machineID)
	if err != nil {
		h.writeDashboardError(w, r, "machine_identifiers", err)
		return
	}
	h.renderDashboard(w, dashboardView{Page: "machines", Title: "Machine", Description: "Discovery identity is inventory data, never authentication.", Session: session, Machine: &item, Identifiers: identifiers})
}

func (h *DashboardHandler) dashboardMachinePolicy(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireDashboardAction(w, r, false)
	if !ok {
		return
	}
	machineID := strings.TrimSpace(r.PathValue("id"))
	policy := machine.Policy(strings.TrimSpace(r.PostForm.Get("policy")))
	if _, err := h.state.SetMachinePolicy(r.Context(), machineID, policy, requestID(r), session.Actor); err != nil {
		h.writeDashboardError(w, r, "machine_policy", err)
		return
	}
	http.Redirect(w, r, "/ui/machines/"+machineID, http.StatusSeeOther)
}

func (h *DashboardHandler) dashboardInstallations(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireDashboardPage(w, r)
	if !ok {
		return
	}
	rows, err := h.loadInstallationRows(r)
	if err != nil {
		h.writeDashboardError(w, r, "installations", err)
		return
	}
	h.renderDashboard(w, dashboardView{Page: "installations", Title: "Installations", Description: "Immutable specifications. Arming a destructive boot remains a separate explicit action.", Session: session, Installations: rows})
}

func (h *DashboardHandler) dashboardInstallation(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireDashboardPage(w, r)
	if !ok {
		return
	}
	installationID := strings.TrimSpace(r.PathValue("id"))
	spec, err := h.state.InstallationSpec(r.Context(), installationID)
	if err != nil {
		h.writeDashboardError(w, r, "installation", err)
		return
	}
	var assigned *assignment.Assignment
	stored, err := h.state.AssignmentForInstallation(r.Context(), installationID)
	if err == nil {
		assigned = &stored
	} else if fault.Code(err) != fault.InstallationAssignmentNotFound {
		h.writeDashboardError(w, r, "installation_assignment", err)
		return
	}
	h.renderDashboard(w, dashboardView{Page: "installations", Title: "Installation", Description: "Review the immutable target state before arming the next PXE boot.", Session: session, Spec: &spec, Assignment: assigned})
}

func (h *DashboardHandler) dashboardInstallationWizard(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireDashboardPage(w, r)
	if !ok {
		return
	}
	machines, err := h.provisionMachines(r)
	if err != nil {
		h.writeDashboardError(w, r, "wizard_machines", err)
		return
	}
	values := defaultWizardValues()
	values.MachineID = strings.TrimSpace(r.URL.Query().Get("machine_id"))
	if values.MachineID == "" && len(machines) > 0 {
		values.MachineID = machines[0].ID
	}
	view := dashboardView{Page: "installations", Title: "New installation", Description: "Debian 13 Standard with server-owned artifact trust and security baseline.", Session: session, Machines: machines, Wizard: values}
	if len(machines) == 0 {
		view.Error = "No PROVISION-approved machine is available. Approve a machine first."
	}
	h.renderDashboard(w, view)
}

func (h *DashboardHandler) dashboardCreateInstallation(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	session, ok := h.requireDashboardAction(w, r, false)
	if !ok {
		return
	}
	values := wizardValuesFromRequest(r)
	item, err := h.state.Machine(r.Context(), values.MachineID)
	if err != nil {
		h.renderDashboardWizardError(w, r, session, values, "Selected machine could not be loaded.", fault.Code(err), "machine_lookup_failed", started)
		return
	}
	if item.Policy != machine.PolicyProvision {
		h.renderDashboardWizardError(w, r, session, values, "Machine must be approved with policy PROVISION before creating an installation.", fault.InstallationSpecInvalid, "machine_not_provision_approved", started)
		return
	}
	if item.Architecture != "" && item.Architecture != "x86_64" && item.Architecture != "amd64" {
		h.renderDashboardWizardError(w, r, session, values, "Debian 13 Standard currently supports amd64/x86_64 machines only.", fault.InstallationSpecInvalid, "machine_architecture_unsupported", started)
		return
	}
	profileSnapshot := profile.Snapshot{
		SchemaVersion: profile.SchemaVersion,
		Hostname:      strings.TrimSpace(values.Hostname),
		Locale:        strings.TrimSpace(values.Locale),
		Keyboard:      strings.TrimSpace(values.Keyboard),
		Timezone:      strings.TrimSpace(values.Timezone),
		Admin: profile.Admin{
			Username:          strings.TrimSpace(values.AdminUsername),
			FullName:          strings.TrimSpace(values.AdminFullName),
			AuthorizedSSHKeys: parseSSHKeys(values.SSHKeys),
			PasswordlessSudo:  true,
		},
		Packages: parsePackages(values.Packages),
	}
	if err := profileSnapshot.Validate(); err != nil {
		h.renderDashboardWizardError(w, r, session, values, "Profile input is invalid: "+err.Error(), fault.InstallationSpecInvalid, "profile_validation_failed", started)
		return
	}
	if err := installation.ValidateTargetDisk(strings.TrimSpace(values.TargetDisk)); err != nil {
		h.renderDashboardWizardError(w, r, session, values, "Target disk is invalid. Use a whole device such as /dev/vda, /dev/sda or /dev/nvme0n1.", fault.InstallationSpecInvalid, "target_disk_invalid", started)
		return
	}

	resolveStarted := time.Now()
	resolution, err := h.resolver.Resolve(r.Context())
	if err != nil {
		code := fault.Code(err)
		if code == "" {
			code = fault.ArtifactTrustFailed
		}
		h.logger.WarnContext(r.Context(), "operator Debian artifact resolution failed", "component", "operator.installation", "operation", "resolve_artifacts", "request_id", requestID(r), "machine_id", item.ID, "actor", session.Actor, "error_code", code, "result", "failure", "cause", err.Error(), "duration_ms", time.Since(resolveStarted).Milliseconds())
		h.renderDashboardWizardError(w, r, session, values, "Debian installer artifacts could not be verified. Check the server log for "+code+".", code, "artifact_resolution_failed", started)
		return
	}
	h.logger.InfoContext(r.Context(), "operator Debian artifacts resolved", "component", "operator.installation", "operation", "resolve_artifacts", "request_id", requestID(r), "machine_id", item.ID, "actor", session.Actor, "release_version", resolution.ReleaseVersion, "installer_version", resolution.InstallerVersion, "kernel_digest", resolution.Kernel.Descriptor.Digest, "initrd_digest", resolution.Initrd.Descriptor.Digest, "result", "success", "duration_ms", time.Since(resolveStarted).Milliseconds())

	credentialID, err := idgen.New("lc_")
	if err != nil {
		h.renderDashboardWizardError(w, r, session, values, "Could not allocate installation identity metadata.", fault.StorageFailure, "credential_id_allocation_failed", started)
		return
	}
	spec := installation.Spec{
		MachineID: item.ID, DriverID: debian13.DriverID, DriverVersion: debian13.DriverVersion, OSRelease: "13", Architecture: "amd64",
		ProfileID: builtinProfileID, ProfileRevision: builtinProfileRevision, Profile: profileSnapshot,
		Artifacts:             []installation.Artifact{resolution.Kernel.Descriptor, resolution.Initrd.Descriptor},
		Storage:               installation.Storage{Mode: "whole-disk", Filesystem: "ext4", TargetDisk: strings.TrimSpace(values.TargetDisk), Encrypted: false, TPM2: false},
		Security:              installation.Security{SSHPasswordAuthentication: false, RootLogin: false, AutomaticSecurityUpdates: true},
		LifecycleCredentialID: credentialID, CreatedBy: session.Actor,
	}
	if err := debian13.ValidateSpec(spec); err != nil {
		h.renderDashboardWizardError(w, r, session, values, "Debian 13 Standard rejected the requested installation: "+err.Error(), fault.DriverSpecUnsupported, "driver_validation_failed", started)
		return
	}
	created, err := h.state.CreateInstallationSpec(r.Context(), spec, requestID(r))
	if err != nil {
		code := fault.Code(err)
		if code == "" {
			code = fault.StorageFailure
		}
		h.renderDashboardWizardError(w, r, session, values, "InstallationSpec could not be created. Check the server log for "+code+".", code, "spec_persistence_failed", started)
		return
	}
	h.logger.InfoContext(r.Context(), "operator created immutable installation spec", "component", "operator.installation", "operation", "create_spec", "request_id", requestID(r), "machine_id", item.ID, "installation_id", created.ID, "actor", session.Actor, "driver_id", created.DriverID, "driver_version", created.DriverVersion, "release_version", resolution.ReleaseVersion, "installer_version", resolution.InstallerVersion, "target_disk", created.Storage.TargetDisk, "result", "success", "duration_ms", time.Since(started).Milliseconds())
	http.Redirect(w, r, "/ui/installations/"+created.ID, http.StatusSeeOther)
}

func (h *DashboardHandler) dashboardArmInstallation(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireDashboardAction(w, r, false)
	if !ok {
		return
	}
	installationID := strings.TrimSpace(r.PathValue("id"))
	spec, err := h.state.InstallationSpec(r.Context(), installationID)
	if err != nil {
		h.writeDashboardError(w, r, "arm_installation", err)
		return
	}
	if _, err := h.state.ArmInstallation(r.Context(), spec.MachineID, spec.ID, requestID(r), session.Actor); err != nil {
		h.writeDashboardError(w, r, "arm_installation", err)
		return
	}
	http.Redirect(w, r, "/ui/installations/"+installationID, http.StatusSeeOther)
}

func (h *DashboardHandler) dashboardCancelInstallation(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireDashboardAction(w, r, false)
	if !ok {
		return
	}
	installationID := strings.TrimSpace(r.PathValue("id"))
	if _, err := h.state.CancelAssignment(r.Context(), installationID, requestID(r), session.Actor); err != nil {
		h.writeDashboardError(w, r, "cancel_installation", err)
		return
	}
	http.Redirect(w, r, "/ui/installations/"+installationID, http.StatusSeeOther)
}

func (h *DashboardHandler) provisionMachines(r *http.Request) ([]machine.Machine, error) {
	items, err := h.state.Machines(r.Context())
	if err != nil {
		return nil, err
	}
	out := make([]machine.Machine, 0, len(items))
	for _, item := range items {
		if item.Policy == machine.PolicyProvision {
			out = append(out, item)
		}
	}
	return out, nil
}

func (h *DashboardHandler) renderDashboardWizardError(w http.ResponseWriter, r *http.Request, session operator.Session, values wizardValues, message, code, cause string, started time.Time) {
	if code == "" {
		code = fault.InstallationSpecInvalid
	}
	h.logger.WarnContext(r.Context(), "operator installation wizard rejected", "component", "operator.installation", "operation", "create_spec", "request_id", requestID(r), "machine_id", values.MachineID, "actor", session.Actor, "error_code", code, "result", "rejected", "cause", cause, "duration_ms", time.Since(started).Milliseconds())
	machines, err := h.provisionMachines(r)
	if err != nil {
		h.writeDashboardError(w, r, "wizard_machines", err)
		return
	}
	h.renderDashboard(w, dashboardView{Page: "installations", Title: "New installation", Description: "Debian 13 Standard with server-owned artifact trust and security baseline.", Session: session, Machines: machines, Wizard: values, Error: message})
}
