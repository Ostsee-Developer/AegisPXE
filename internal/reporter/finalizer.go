package reporter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/lifecycle"
)

const (
	systemStateDir       = "/var/lib/aegispxe"
	systemCiphertextPath = systemStateDir + "/lifecycle.enc"
	systemLogSequence    = systemStateDir + "/log.sequence"
	firstBootUnitPath    = "/etc/systemd/system/aegispxe-firstboot.service"
	reporterInstallPath  = "/usr/local/lib/aegispxe/aegispxe-reporter"
)

const firstBootUnit = `[Unit]
Description=AegisPXE first-boot validation reporter
After=network-online.target
Wants=network-online.target
ConditionPathExists=/var/lib/aegispxe/lifecycle.enc

[Service]
Type=oneshot
ExecStart=/usr/local/lib/aegispxe/aegispxe-reporter first-boot
TimeoutStartSec=15min

[Install]
WantedBy=multi-user.target
`

func InstallFirstBoot(ctx context.Context, cfg Config, target string) error {
	target = filepath.Clean(strings.TrimSpace(target))
	if target == "." || !filepath.IsAbs(target) || target == "/" {
		return errors.New("first-boot target must be an absolute non-root installation path")
	}
	if _, err := WaitForCredentialCiphertext(ctx, 15*time.Minute); err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Minute)
	for !QueueEmpty() && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	if !QueueEmpty() {
		return errors.New("reporter lifecycle queue did not drain before first-boot handoff")
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if err := copyFile(executable, target+reporterInstallPath, 0755); err != nil {
		return fmt.Errorf("install first-boot reporter: %w", err)
	}
	configBytes, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	configBytes = append(configBytes, '\n')
	if err := writeTargetFile(target+SystemConfigPath, configBytes, 0600); err != nil {
		return err
	}
	ciphertext, err := os.ReadFile(ciphertextPath)
	if err != nil {
		return fmt.Errorf("read encrypted lifecycle credential: %w", err)
	}
	if err := writeTargetFile(target+systemCiphertextPath, ciphertext, 0600); err != nil {
		return err
	}
	if sequence, err := os.ReadFile(logSequencePath); err == nil && len(sequence) > 0 {
		if err := writeTargetFile(target+systemLogSequence, sequence, 0600); err != nil {
			return err
		}
	}
	if err := writeTargetFile(target+firstBootUnitPath, []byte(firstBootUnit), 0644); err != nil {
		return err
	}
	wants := target + "/etc/systemd/system/multi-user.target.wants"
	if err := os.MkdirAll(wants, 0755); err != nil {
		return err
	}
	link := filepath.Join(wants, "aegispxe-firstboot.service")
	_ = os.Remove(link)
	if err := os.Symlink("../aegispxe-firstboot.service", link); err != nil {
		return err
	}
	return nil
}

func RunFirstBoot(ctx context.Context, cfg Config) error {
	ciphertext, err := os.ReadFile(systemCiphertextPath)
	if err != nil {
		return fmt.Errorf("read first-boot credential ciphertext: %w", err)
	}
	key, err := OpenTPMKey()
	if err != nil {
		return fmt.Errorf("open TPM reporter key at first boot: %w", err)
	}
	defer key.Close()
	secret, err := key.DecryptLifecycleCredential(ciphertext)
	if err != nil {
		return err
	}
	client := NewClient(cfg)

	if err := client.PostEvent(ctx, secret, "reporter:first-boot", lifecycle.StageFirstBoot, lifecycle.SourceFinalizer, "Installed operating system reached first boot", "", time.Now().UTC()); err != nil {
		return err
	}
	if err := client.PostEvent(ctx, secret, "reporter:validating", lifecycle.StageValidating, lifecycle.SourceValidator, "AegisPXE first-boot validation started", "", time.Now().UTC()); err != nil {
		return err
	}

	validationLog, validationErr := validateInstalledSystem(cfg)
	if err := postFirstBootLog(ctx, client, secret, validationLog); err != nil {
		return err
	}
	if validationErr != nil {
		if err := client.PostEvent(ctx, secret, "reporter:failed:firstboot-validation", lifecycle.StageFailed, lifecycle.SourceValidator, validationErr.Error(), "VAL001_FIRST_BOOT_VALIDATION_FAILED", time.Now().UTC()); err != nil {
			return err
		}
		_ = os.Remove(systemCiphertextPath)
		return validationErr
	}
	if err := client.PostEvent(ctx, secret, "reporter:success", lifecycle.StageSuccess, lifecycle.SourceValidator, "AegisPXE first-boot validation completed successfully", "", time.Now().UTC()); err != nil {
		return err
	}
	if err := os.Remove(systemCiphertextPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func validateInstalledSystem(cfg Config) (string, error) {
	var log strings.Builder
	fail := func(message string) (string, error) {
		log.WriteString("FAIL ")
		log.WriteString(message)
		log.WriteByte('\n')
		return log.String(), errors.New(message)
	}

	passwd, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return fail("could not read /etc/passwd")
	}
	prefix := cfg.AdminUsername + ":"
	foundUser := false
	for _, line := range strings.Split(string(passwd), "\n") {
		if strings.HasPrefix(line, prefix) {
			foundUser = true
			break
		}
	}
	if !foundUser {
		return fail("configured administrator account is missing")
	}
	log.WriteString("PASS administrator account exists\n")

	authorizedKeys := "/home/" + cfg.AdminUsername + "/.ssh/authorized_keys"
	info, err := os.Stat(authorizedKeys)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return fail("administrator authorized_keys is missing or empty")
	}
	if info.Mode().Perm()&0077 != 0 {
		return fail("administrator authorized_keys permissions are too broad")
	}
	log.WriteString("PASS administrator SSH authorized_keys exists with restricted permissions\n")

	configInfo, err := os.Stat("/etc/ssh/sshd_config.d/90-aegispxe.conf")
	if err != nil || !configInfo.Mode().IsRegular() {
		return fail("AegisPXE SSH hardening configuration is missing")
	}
	log.WriteString("PASS SSH hardening configuration exists\n")

	if output, err := exec.Command("/usr/sbin/sshd", "-t").CombinedOutput(); err != nil {
		log.WriteString("FAIL sshd -t failed\n")
		if len(output) > 0 {
			log.WriteString("sshd reported configuration errors\n")
		}
		return log.String(), errors.New("OpenSSH configuration validation failed")
	}
	log.WriteString("PASS sshd -t\n")

	if _, err := os.Stat("/etc/sudoers.d/90-aegispxe-admin"); err != nil {
		return fail("AegisPXE sudo policy is missing")
	}
	log.WriteString("PASS AegisPXE sudo policy exists\n")
	return log.String(), nil
}

func postFirstBootLog(ctx context.Context, client *Client, secret, content string) error {
	sequence := readIntFile(systemLogSequence) + 1
	if sequence <= 0 {
		sequence = 1
	}
	if err := client.PostLog(ctx, secret, fmt.Sprintf("reporter:firstboot-log:%d", sequence), sequence, lifecycle.SourceValidator, content, time.Now().UTC()); err != nil {
		return err
	}
	return writeIntFile(systemLogSequence, sequence)
}

func copyFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	tmp := destination + ".tmp"
	output, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, destination)
}

func writeTargetFile(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
