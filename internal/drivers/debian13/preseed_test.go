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
	for _, want := range []string{
		"d-i partman-auto/disk string /dev/vda",
		"d-i grub-installer/bootdev string /dev/vda",
		"-p NP guardian",
		"PasswordAuthentication no",
		"KbdInteractiveAuthentication no",
		"PermitRootLogin no",
		"NOPASSWD: ALL",
		"in-target sshd -t",
		"in-target visudo -cf /etc/sudoers.d/90-aegispxe-admin",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("preseed missing %q", want)
		}
	}
	if strings.Contains(content, spec.LifecycleCredentialID) || strings.Contains(content, "preseed/url") {
		t.Fatal("preseed contains lifecycle credential identity or transport URL")
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
