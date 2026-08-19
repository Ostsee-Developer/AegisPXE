package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/lifecycle"
	"github.com/Ostsee-Developer/AegisPXE/internal/telemetryauth"
)

func TestReporterTelemetryAcceptsValidMACAndRejectsBodyTamper(t *testing.T) {
	state, _ := testServer(t)
	_, spec := createArmedProvisioningState(t, state, "52:54:00:40:00:71")
	ctx := context.Background()
	if _, _, err := state.AppendLifecycleEvent(ctx, lifecycle.Report{
		InstallationID: spec.ID,
		Stage:          lifecycle.StagePXEBooted,
		Source:         lifecycle.SourceServer,
		IdempotencyKey: "server-pxe-reporter-test",
		Message:        "PXE booted for reporter test",
	}, "req_pxe_reporter_test"); err != nil {
		t.Fatal(err)
	}
	issued, err := state.IssueLifecycleCredential(ctx, spec.ID, "req_issue_reporter_test", "system:test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	server := New(state, logger, "test")
	handler := server.HandlerWithBootTrust()

	body, err := json.Marshal(map[string]any{
		"stage":   lifecycle.StageInstallerStarted,
		"source":  lifecycle.SourceInstaller,
		"message": "Debian Installer started",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/installations/" + spec.ID + "/reporter/events"
	idempotencyKey := "reporter-test-installer-started"
	timestamp := time.Now().UTC().Unix()
	canonical, err := telemetryauth.Canonical(http.MethodPost, path, idempotencyKey, timestamp, body)
	if err != nil {
		t.Fatal(err)
	}
	key := telemetryauth.KeyFromSecret(issued.Secret)
	signature := telemetryauth.Sign(key[:], canonical)

	req := httptest.NewRequest(http.MethodPost, "http://aegispxe.test"+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)
	req.Header.Set("X-Aegis-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("Authorization", telemetryauth.Scheme+" "+signature)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("signed reporter event status=%d body=%s", rec.Code, rec.Body.String())
	}
	stage, err := state.CurrentLifecycleStage(ctx, spec.ID)
	if err != nil || stage != lifecycle.StageInstallerStarted {
		t.Fatalf("current lifecycle stage=%s err=%v", stage, err)
	}

	tampered := []byte(`{"stage":"FAILED","source":"installer","error_code":"INS999_TAMPERED"}`)
	req = httptest.NewRequest(http.MethodPost, "http://aegispxe.test"+path, bytes.NewReader(tampered))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)
	req.Header.Set("X-Aegis-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("Authorization", telemetryauth.Scheme+" "+signature)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("tampered reporter event status=%d body=%s", rec.Code, rec.Body.String())
	}
}
