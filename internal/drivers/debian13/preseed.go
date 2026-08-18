package debian13

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/driver"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/installation"
)

func RenderSeed(ctx context.Context, logger *slog.Logger, spec installation.Spec, requestID string) (driver.SeedBundle, error) {
	started := time.Now()
	if logger == nil {
		return driver.SeedBundle{}, fault.New(fault.DriverRenderFailed, "driver logger is required", nil)
	}
	if strings.TrimSpace(requestID) == "" {
		return driver.SeedBundle{}, fault.New(fault.DriverRenderFailed, "driver render request identifier is required", nil)
	}
	if err := ValidateSpec(spec); err != nil {
		logger.WarnContext(ctx, "Debian seed render rejected", "component", "driver.debian13", "operation", "render_seed", "request_id", requestID, "machine_id", spec.MachineID, "installation_id", spec.ID, "driver_id", DriverID, "driver_version", DriverVersion, "target_disk", spec.Storage.TargetDisk, "result", "rejected", "error_code", fault.DriverSpecUnsupported, "cause", err.Error(), "duration_ms", time.Since(started).Milliseconds())
		return driver.SeedBundle{}, fault.New(fault.DriverSpecUnsupported, "installation spec is unsupported by Debian 13 Standard", err)
	}

	bundle, err := renderPreseed(spec)
	if err != nil {
		logger.ErrorContext(ctx, "Debian seed render failed", "component", "driver.debian13", "operation", "render_seed", "request_id", requestID, "machine_id", spec.MachineID, "installation_id", spec.ID, "driver_id", DriverID, "driver_version", DriverVersion, "target_disk", spec.Storage.TargetDisk, "result", "failure", "error_code", fault.DriverRenderFailed, "cause", err.Error(), "duration_ms", time.Since(started).Milliseconds())
		return driver.SeedBundle{}, fault.New(fault.DriverRenderFailed, "could not render Debian installer seed", err)
	}

	logger.InfoContext(ctx, "Debian seed rendered", "component", "driver.debian13", "operation", "render_seed", "request_id", requestID, "machine_id", spec.MachineID, "installation_id", spec.ID, "driver_id", DriverID, "driver_version", DriverVersion, "profile_id", spec.ProfileID, "profile_revision", spec.ProfileRevision, "target_disk", spec.Storage.TargetDisk, "seed_bytes", len(bundle.Content), "result", "success", "duration_ms", time.Since(started).Milliseconds())
	return bundle.Clone(), nil
}

func renderPreseed(spec installation.Spec) (driver.SeedBundle, error) {
	packages := seedPackages(spec)
	lateCommand, err := renderLateCommand(spec)
	if err != nil {
		return driver.SeedBundle{}, err
	}

	var out strings.Builder
	lines := []string{
		"# AegisPXE Debian 13 Standard preseed",
		"d-i debian-installer/locale string " + spec.Profile.Locale,
		"d-i keyboard-configuration/xkb-keymap select " + spec.Profile.Keyboard,
		"d-i netcfg/choose_interface select auto",
		"d-i netcfg/get_hostname string " + spec.Profile.Hostname,
		"d-i netcfg/hostname string " + spec.Profile.Hostname,
		"d-i netcfg/get_domain string",
		"d-i netcfg/wireless_wep string",
		"d-i mirror/country string manual",
		"d-i mirror/http/hostname string deb.debian.org",
		"d-i mirror/http/directory string /debian",
		"d-i mirror/http/proxy string",
		"d-i passwd/root-login boolean false",
		"d-i passwd/user-fullname string " + spec.Profile.Admin.FullName,
		"d-i passwd/username string " + spec.Profile.Admin.Username,
		"d-i passwd/user-password-crypted password !",
		"d-i clock-setup/utc boolean true",
		"d-i time/zone string " + spec.Profile.Timezone,
		"d-i clock-setup/ntp boolean true",
		"d-i partman-auto/disk string " + spec.Storage.TargetDisk,
		"d-i partman-auto/method string regular",
		"d-i partman-auto/choose_recipe select atomic",
		"d-i partman-lvm/device_remove_lvm boolean true",
		"d-i partman-md/device_remove_md boolean true",
		"d-i partman-lvm/confirm boolean true",
		"d-i partman-lvm/confirm_nooverwrite boolean true",
		"d-i partman-partitioning/confirm_write_new_label boolean true",
		"d-i partman/choose_partition select finish",
		"d-i partman/confirm boolean true",
		"d-i partman/confirm_nooverwrite boolean true",
		"d-i apt-setup/cdrom/set-first boolean false",
		"d-i apt-setup/services-select multiselect security, updates",
		"tasksel tasksel/first multiselect standard",
		"d-i pkgsel/include string " + strings.Join(packages, " "),
		"d-i pkgsel/upgrade select safe-upgrade",
		"popularity-contest popularity-contest/participate boolean false",
		"d-i grub-installer/only_debian boolean true",
		"d-i grub-installer/with_other_os boolean false",
		"d-i grub-installer/bootdev string " + spec.Storage.TargetDisk,
		"d-i preseed/late_command string " + lateCommand,
		"d-i finish-install/reboot_in_progress note",
		"d-i cdrom-detect/eject boolean false",
	}
	for _, line := range lines {
		out.WriteString(line)
		out.WriteByte('\n')
	}

	bundle := driver.SeedBundle{
		Filename:  "preseed.cfg",
		MediaType: "text/plain; charset=utf-8",
		Content:   []byte(out.String()),
	}
	if err := bundle.Validate(); err != nil {
		return driver.SeedBundle{}, fmt.Errorf("invalid rendered seed bundle: %w", err)
	}
	return bundle, nil
}

func seedPackages(spec installation.Spec) []string {
	set := map[string]struct{}{
		"openssh-server":      {},
		"sudo":                {},
		"unattended-upgrades": {},
	}
	for _, name := range spec.Profile.Packages {
		set[name] = struct{}{}
	}
	packages := make([]string, 0, len(set))
	for name := range set {
		packages = append(packages, name)
	}
	sort.Strings(packages)
	return packages
}

func renderLateCommand(spec installation.Spec) (string, error) {
	username := spec.Profile.Admin.Username
	keys := make([]string, 0, len(spec.Profile.Admin.AuthorizedSSHKeys))
	for _, value := range spec.Profile.Admin.AuthorizedSSHKeys {
		fields := strings.Fields(value)
		if len(fields) < 2 {
			return "", errors.New("validated SSH public key became unparsable")
		}
		keys = append(keys, fields[0]+" "+fields[1])
	}

	keyArgs := make([]string, 0, len(keys))
	for _, key := range keys {
		keyArgs = append(keyArgs, shellQuote(key))
	}

	commands := []string{
		"set -e",
		"in-target install -d -m 0700 -o " + username + " -g " + username + " /home/" + username + "/.ssh",
		"printf '%s\\n' " + strings.Join(keyArgs, " ") + " > /target/home/" + username + "/.ssh/authorized_keys",
		"in-target chown " + username + ":" + username + " /home/" + username + "/.ssh/authorized_keys",
		"in-target chmod 0600 /home/" + username + "/.ssh/authorized_keys",
		"in-target usermod -aG sudo " + username,
		"printf '%s\\n' " + shellQuote(username+" ALL=(ALL:ALL) NOPASSWD: ALL") + " > /target/etc/sudoers.d/90-aegispxe-admin",
		"chmod 0440 /target/etc/sudoers.d/90-aegispxe-admin",
		"in-target visudo -cf /etc/sudoers.d/90-aegispxe-admin",
		"mkdir -p /target/etc/ssh/sshd_config.d",
		"printf '%s\\n' 'PasswordAuthentication no' 'KbdInteractiveAuthentication no' 'PermitEmptyPasswords no' 'PermitRootLogin no' 'PubkeyAuthentication yes' > /target/etc/ssh/sshd_config.d/90-aegispxe.conf",
		"chmod 0644 /target/etc/ssh/sshd_config.d/90-aegispxe.conf",
		"in-target usermod -p NP " + username,
		"in-target sshd -t",
		"printf '%s\\n' 'APT::Periodic::Update-Package-Lists \"1\";' 'APT::Periodic::Unattended-Upgrade \"1\";' > /target/etc/apt/apt.conf.d/20auto-upgrades",
		"chmod 0644 /target/etc/apt/apt.conf.d/20auto-upgrades",
	}
	return strings.Join(commands, "; "), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
