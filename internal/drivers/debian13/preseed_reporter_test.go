package debian13

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/observability"
)

func TestRenderSeedWiresReporterHooksWithoutLifecycleSecret(t *testing.T) {
	spec := validInstallationSpec()
	var logs bytes.Buffer
	seed, err := RenderSeed(context.Background(), observability.New(&logs, slog.LevelDebug), spec, "req_reporter_hooks")
	if err != nil {
		t.Fatal(err)
	}
	content := string(seed.Content)
	for _, want := range []string{
		"d-i preseed/early_command string chmod 0755 /aegispxe/reporter",
		"/aegispxe/reporter daemon --config /aegispxe/reporter.json",
		"INSTALLER_STARTED",
		"d-i partman/early_command string /aegispxe/reporter event",
		"DISK_PREPARATION",
		"/aegispxe/reporter event --message 'AegisPXE hardening started' HARDENING",
		"INS104_HARDENING_FAILED FAILED",
		"/aegispxe/reporter install-firstboot --config /aegispxe/reporter.json /target",
		"trap - EXIT",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("reporter-enabled preseed missing %q", want)
		}
	}
	if strings.Contains(content, spec.LifecycleCredentialID) || strings.Contains(strings.ToLower(content), "bearer ") || strings.Contains(strings.ToLower(content), "lifecycle_secret") {
		t.Fatal("reporter-enabled Preseed leaked lifecycle credential material")
	}
}
