package debian13

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/observability"
)

func TestRenderSeedPinsDiskAndKeyOnlyAdmin(t *testing.T) {
	spec := validInstallationSpec()
	var logs bytes.Buffer
	logger := observability.New(&logs, slog.LevelDebug)

	seed, err := RenderSeed(context.Background(), logger, spec, "req_seed")
	if err != nil {
		t.Fatal(err)
	}
	content := string(seed.Content)
	if !strings.HasPrefix(content, "#_preseed_V1\n") {
		t.Fatalf("preseed missing canonical v1 header: %q", content)
	}
	for _, want := range []string{
		"d-i partman-auto/disk string /dev/vda",
		"d-i grub-installer/bootdev string /dev/vda",
		"d-i passwd/user-fullname string Aegis Administrator",
		"d-i passwd/username string guardian",
		"d-i passwd/user-password-crypted password !",
		"in-target usermod -p NP guardian",
		"PasswordAuthentication no",
		"KbdInteractiveAuthentication no",
		"PermitRootLogin no",
		"NOPASSWD: ALL",
		"in-target sshd -t",
		"in-target visudo -cf /etc/sudoers.d/90-aegispxe-admin",
		"d-i finish-install/reboot_in_progress note",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("preseed missing %q", want)
		}
	}
	if strings.Contains(content, spec.LifecycleCredentialID) || strings.Contains(content, "preseed/url") {
		t.Fatal("preseed contains lifecycle credential identity or transport URL")
	}
}

func TestRenderSeedLateHookWritesDeterministicStepMarkers(t *testing.T) {
	spec := validInstallationSpec()
	var logs bytes.Buffer
	logger := observability.New(&logs, slog.LevelDebug)

	seed, err := RenderSeed(context.Background(), logger, spec, "req_late_hook")
	if err != nil {
		t.Fatal(err)
	}
	content := string(seed.Content)
	for _, want := range []string{
		"/target/var/log/aegispxe-installer.log",
		"component=aegispxe step=late_command result=started",
		"component=aegispxe step=authorized_keys result=started",
		"component=aegispxe step=validate_sudoers result=started",
		"component=aegispxe step=prepare_sshd_runtime result=started",
		"in-target install -d -m 0755 /run/sshd",
		"in-target ssh-keygen -A",
		"component=aegispxe step=validate_sshd result=started",
		"component=aegispxe step=automatic_updates result=success",
		"component=aegispxe step=late_command result=success",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("late hook missing %q", want)
		}
	}
	marker := strings.Index(content, "component=aegispxe step=late_command result=started")
	firstHardeningCommand := strings.Index(content, "in-target install -d -m 0700")
	if marker < 0 || firstHardeningCommand < 0 || marker > firstHardeningCommand {
		t.Fatal("late hook must persist its first marker before target hardening starts")
	}
}

func TestRenderSeedLateHookFailsClosedWithoutComplexExitTrap(t *testing.T) {
	spec := validInstallationSpec()
	var logs bytes.Buffer
	logger := observability.New(&logs, slog.LevelDebug)

	seed, err := RenderSeed(context.Background(), logger, spec, "req_late_hook_fail_closed")
	if err != nil {
		t.Fatal(err)
	}
	content := string(seed.Content)
	if !strings.Contains(content, "d-i preseed/late_command string set -e;") {
		t.Fatal("late hook must fail closed with set -e")
	}
	for _, forbidden := range []string{"|| true", "trap '", "AEGIS_RESULT="} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("late hook contains fragile or failure-masking shell construct %q", forbidden)
		}
	}
}

func TestRenderSeedLateHookHasNoDebconfEscapeSequences(t *testing.T) {
	spec := validInstallationSpec()
	var logs bytes.Buffer
	logger := observability.New(&logs, slog.LevelDebug)

	seed, err := RenderSeed(context.Background(), logger, spec, "req_late_hook_escape_safety")
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "d-i preseed/late_command string "
	var lateCommand string
	for _, line := range strings.Split(string(seed.Content), "\n") {
		if strings.HasPrefix(line, prefix) {
			lateCommand = strings.TrimPrefix(line, prefix)
			break
		}
	}
	if lateCommand == "" {
		t.Fatal("preseed late command is missing")
	}
	if strings.ContainsAny(lateCommand, "\\\r\n") {
		t.Fatalf("late command contains an escape or line break that debconf can reinterpret: %q", lateCommand)
	}
	for _, want := range []string{
		"echo 'component=aegispxe step=late_command result=started'",
		": > '/target/home/guardian/.ssh/authorized_keys'",
		"echo 'PasswordAuthentication no' > '/target/etc/ssh/sshd_config.d/90-aegispxe.conf'",
		"echo 'APT::Periodic::Unattended-Upgrade \"1\";' >> '/target/etc/apt/apt.conf.d/20auto-upgrades'",
	} {
		if !strings.Contains(lateCommand, want) {
			t.Fatalf("late command missing preseed-safe fragment %q", want)
		}
	}
}

func TestRenderSeedLogsCorrelationWithoutSeedOrKeyMaterial(t *testing.T) {
	spec := validInstallationSpec()
	var logs bytes.Buffer
	logger := observability.New(&logs, slog.LevelDebug)

	seed, err := RenderSeed(context.Background(), logger, spec, "req_seed_log")
	if err != nil {
		t.Fatal(err)
	}
	logText := logs.String()
	keyPayload := strings.Fields(spec.Profile.Admin.AuthorizedSSHKeys[0])[1]
	if !strings.Contains(logText, `"operation":"render_seed"`) || !strings.Contains(logText, `"request_id":"req_seed_log"`) || !strings.Contains(logText, `"installation_id":"i_test"`) || !strings.Contains(logText, `"result":"success"`) {
		t.Fatalf("render log lacks correlation fields: %s", logText)
	}
	if strings.Contains(logText, keyPayload) || strings.Contains(logText, string(seed.Content)) {
		t.Fatal("render log leaked SSH key or seed content")
	}
}

func TestRenderSeedRejectsUnsupportedSpecWithStableCode(t *testing.T) {
	spec := validInstallationSpec()
	spec.Storage.Encrypted = true
	var logs bytes.Buffer
	logger := observability.New(&logs, slog.LevelDebug)

	_, err := RenderSeed(context.Background(), logger, spec, "req_seed_reject")
	if fault.Code(err) != fault.DriverSpecUnsupported {
		t.Fatalf("code=%q err=%v", fault.Code(err), err)
	}
	if !strings.Contains(logs.String(), fault.DriverSpecUnsupported) || !strings.Contains(logs.String(), `"result":"rejected"`) {
		t.Fatalf("rejection is not observably logged: %s", logs.String())
	}
}
