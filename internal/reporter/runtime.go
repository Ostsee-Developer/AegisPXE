package reporter

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/lifecycle"
)

const (
	runtimeDir         = "/run/aegispxe"
	queuePath          = runtimeDir + "/events.queue"
	ciphertextPath     = runtimeDir + "/lifecycle.enc"
	syslogOffsetPath   = runtimeDir + "/syslog.offset"
	logSequencePath    = runtimeDir + "/log.sequence"
	evidenceStatePath  = runtimeDir + "/evidence.state"
	installerSyslog    = "/var/log/syslog"
	maxReporterLogRead = 64 << 10
)

type queuedEvent struct {
	Stage          lifecycle.Stage  `json:"stage"`
	Source         lifecycle.Source `json:"source"`
	Message        string           `json:"message"`
	ErrorCode      string           `json:"error_code,omitempty"`
	ClientTime     time.Time        `json:"client_time"`
	IdempotencyKey string           `json:"idempotency_key"`
}

func QueueEvent(stage lifecycle.Stage, source lifecycle.Source, message, errorCode string) error {
	if err := os.MkdirAll(runtimeDir, 0700); err != nil {
		return err
	}
	key := "reporter:stage:" + strings.ToLower(string(stage))
	if stage == lifecycle.StageFailed {
		key += ":" + sanitizeKey(errorCode)
	}
	item := queuedEvent{Stage: stage, Source: source, Message: strings.TrimSpace(message), ErrorCode: strings.TrimSpace(errorCode), ClientTime: time.Now().UTC(), IdempotencyKey: key}
	if err := validateQueuedEvent(item); err != nil {
		return err
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(queuePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_, err = file.Write(append(encoded, '\n'))
	return err
}

func RunDaemon(ctx context.Context, cfg Config) error {
	if err := os.MkdirAll(runtimeDir, 0700); err != nil {
		return err
	}
	key, err := OpenTPMKey()
	if err != nil {
		_ = QueueEvent(lifecycle.StageFailed, lifecycle.SourceInstaller, "TPM 2.0 boot trust unavailable", "SEC019_BOOT_TRUST_KEY_INVALID")
		return err
	}
	defer key.Close()
	client := NewClient(cfg)

	var secret string
	lastPendingNotice := time.Time{}
	for secret == "" {
		material, err := client.AcquireCredential(ctx, key)
		if err == nil {
			secret = material.Secret
			if err := writePrivateFile(ciphertextPath, material.Ciphertext); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "AegisPXE reporter: TPM trust verified for %s\n", material.Fingerprint)
			break
		}
		if !errors.Is(err, ErrPendingApproval) {
			fmt.Fprintf(os.Stderr, "AegisPXE reporter: trust attempt failed: %v\n", err)
		} else if lastPendingNotice.IsZero() || time.Since(lastPendingNotice) >= 30*time.Second {
			fmt.Fprintf(os.Stderr, "AegisPXE reporter: approve TPM key %s for machine %s in Studio\n", key.Fingerprint(), cfg.MachineID)
			lastPendingNotice = time.Now()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := detectNativeInstallerEvidence(); err != nil {
			fmt.Fprintf(os.Stderr, "AegisPXE reporter: evidence monitor: %v\n", err)
		}
		if err := drainEvents(ctx, client, secret); err != nil {
			fmt.Fprintf(os.Stderr, "AegisPXE reporter: event upload: %v\n", err)
		}
		if err := uploadInstallerLog(ctx, client, secret); err != nil {
			fmt.Fprintf(os.Stderr, "AegisPXE reporter: log upload: %v\n", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func detectNativeInstallerEvidence() error {
	content, err := os.ReadFile(installerSyslog)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	text := string(content)
	seen, _ := readEvidenceState()
	checks := []struct {
		name      string
		needle    string
		stage     lifecycle.Stage
		message   string
		errorCode string
	}{
		{"os-install", "Menu item 'bootstrap-base' selected", lifecycle.StageOSInstalling, "Debian Installer selected bootstrap-base", ""},
		{"profile", "Menu item 'pkgsel' selected", lifecycle.StageProfileApplying, "Debian Installer selected pkgsel", ""},
		{"base-failed", "Menu item 'bootstrap-base' failed", lifecycle.StageFailed, "Debian base installation failed", "INS101_DEBIAN_BASE_INSTALL_FAILED"},
		{"profile-failed", "Menu item 'pkgsel' failed", lifecycle.StageFailed, "Debian package/profile installation failed", "INS102_DEBIAN_PROFILE_INSTALL_FAILED"},
		{"bootloader-failed", "Menu item 'grub-installer' failed", lifecycle.StageFailed, "Debian bootloader installation failed", "INS103_DEBIAN_BOOTLOADER_FAILED"},
	}
	changed := false
	for _, check := range checks {
		if seen[check.name] || !strings.Contains(text, check.needle) {
			continue
		}
		if err := QueueEvent(check.stage, lifecycle.SourceInstaller, check.message, check.errorCode); err != nil {
			return err
		}
		seen[check.name] = true
		changed = true
	}
	if changed {
		return writeEvidenceState(seen)
	}
	return nil
}

func drainEvents(ctx context.Context, client *Client, secret string) error {
	file, err := os.OpenFile(queuePath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	var events []queuedEvent
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var item queuedEvent
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			return fmt.Errorf("decode queued event: %w", err)
		}
		events = append(events, item)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	consumed := 0
	for _, item := range events {
		if err := client.PostEvent(ctx, secret, item.IdempotencyKey, item.Stage, item.Source, item.Message, item.ErrorCode, item.ClientTime); err != nil {
			break
		}
		consumed++
	}
	if consumed == 0 {
		return nil
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	writer := bufio.NewWriter(file)
	for _, item := range events[consumed:] {
		encoded, _ := json.Marshal(item)
		_, _ = writer.Write(append(encoded, '\n'))
	}
	return writer.Flush()
}

func uploadInstallerLog(ctx context.Context, client *Client, secret string) error {
	file, err := os.Open(installerSyslog)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()
	offset := readIntFile(syslogOffsetPath)
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	buffer := make([]byte, maxReporterLogRead)
	n, readErr := file.Read(buffer)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return readErr
	}
	if n == 0 {
		return nil
	}
	sequence := readIntFile(logSequencePath) + 1
	if sequence <= 0 {
		sequence = 1
	}
	idempotency := fmt.Sprintf("reporter:log:%d", sequence)
	if err := client.PostLog(ctx, secret, idempotency, sequence, lifecycle.SourceInstaller, string(buffer[:n]), time.Now().UTC()); err != nil {
		return err
	}
	if err := writeIntFile(syslogOffsetPath, offset+int64(n)); err != nil {
		return err
	}
	return writeIntFile(logSequencePath, sequence)
}

func QueueEmpty() bool {
	info, err := os.Stat(queuePath)
	return err == nil && info.Size() == 0
}

func WaitForCredentialCiphertext(ctx context.Context, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	for {
		content, err := os.ReadFile(ciphertextPath)
		if err == nil && len(content) > 0 {
			return content, nil
		}
		if time.Now().After(deadline) {
			return nil, errors.New("timed out waiting for operator-approved TPM boot trust")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func validateQueuedEvent(item queuedEvent) error {
	if !item.Stage.Valid() || !item.Source.Valid() || strings.TrimSpace(item.IdempotencyKey) == "" {
		return errors.New("invalid reporter lifecycle event")
	}
	if item.Stage == lifecycle.StageFailed && item.ErrorCode == "" {
		return errors.New("FAILED reporter event requires an error code")
	}
	return nil
}

func sanitizeKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var out strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == ':' || r == '-' {
			out.WriteRune(r)
		}
	}
	if out.Len() == 0 {
		return "unknown"
	}
	return out.String()
}

func readEvidenceState() (map[string]bool, error) {
	out := map[string]bool{}
	content, err := os.ReadFile(evidenceStatePath)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	for _, line := range strings.Split(string(content), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out[line] = true
		}
	}
	return out, nil
}

func writeEvidenceState(values map[string]bool) error {
	var lines []string
	for key, value := range values {
		if value {
			lines = append(lines, key)
		}
	}
	return writePrivateFile(evidenceStatePath, []byte(strings.Join(lines, "\n")+"\n"))
}

func readIntFile(path string) int64 {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	value, _ := strconv.ParseInt(strings.TrimSpace(string(content)), 10, 64)
	return value
}

func writeIntFile(path string, value int64) error {
	return writePrivateFile(path, []byte(strconv.FormatInt(value, 10)+"\n"))
}

func writePrivateFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, 0600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
