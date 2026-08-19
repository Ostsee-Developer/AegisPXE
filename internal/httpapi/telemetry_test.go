package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/lifecycle"
)

func TestTelemetryRequiresInstallationScopedCredential(t *testing.T) {
	state, handler := testServer(t)
	_, spec := createArmedProvisioningState(t, state, "52:54:00:40:10:01")
	body := []byte(`{"stage":"INSTALLER_STARTED","source":"installer"}`)

	req := httptest.NewRequest(http.MethodPost, "http://aegispxe.test/api/v1/installations/"+spec.ID+"/telemetry/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "evt-start-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), fault.InstallerCredentialRequired) {
		t.Fatalf("missing credential status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "http://aegispxe.test/api/v1/installations/"+spec.ID+"/telemetry/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "evt-start-2")
	req.Header.Set("Authorization", "Bearer definitely-wrong")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), fault.InstallerCredentialInvalid) {
		t.Fatalf("wrong credential status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuthenticatedLifecycleTelemetryIsMonotonicAndIdempotent(t *testing.T) {
	state, handler := testServer(t)
	_, spec := createArmedProvisioningState(t, state, "52:54:00:40:10:02")
	ctx := context.Background()
	if _, _, err := state.RecordServerLifecycle(ctx, spec.ID, lifecycle.StagePXEBooted, "test:pxe", "PXE reached installation", "req_pxe"); err != nil {
		t.Fatal(err)
	}
	issued, err := state.IssueLifecycleCredential(ctx, spec.ID, "req_issue", "system:test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	post := func(key, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "http://aegispxe.test/api/v1/installations/"+spec.ID+"/telemetry/events", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+issued.Secret)
		req.Header.Set("Idempotency-Key", key)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	body := `{"stage":"INSTALLER_STARTED","source":"installer","message":"debian-installer started","metadata":{"driver":"debian13"}}`
	rec := post("evt-start", body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start status=%d body=%s", rec.Code, rec.Body.String())
	}
	var accepted map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	if accepted["stage"] != string(lifecycle.StageInstallerStarted) || accepted["duplicate"] != false {
		t.Fatalf("unexpected accepted response: %#v", accepted)
	}

	rec = post("evt-start", body)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"duplicate":true`) {
		t.Fatalf("replay status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = post("evt-skip", `{"stage":"OS_INSTALLING","source":"installer"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), fault.InstallerTelemetryConflict) {
		t.Fatalf("skip status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = post("evt-wrong-source", `{"stage":"DISK_PREPARATION","source":"server"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), fault.InstallerTelemetryInvalid) {
		t.Fatalf("source status=%d body=%s", rec.Code, rec.Body.String())
	}

	stage, err := state.CurrentLifecycleStage(ctx, spec.ID)
	if err != nil || stage != lifecycle.StageInstallerStarted {
		t.Fatalf("current stage=%s err=%v", stage, err)
	}
}

func TestAuthenticatedInstallerLogsAreSequencedAndRedacted(t *testing.T) {
	state, handler := testServer(t)
	_, spec := createArmedProvisioningState(t, state, "52:54:00:40:10:03")
	ctx := context.Background()
	issued, err := state.IssueLifecycleCredential(ctx, spec.ID, "req_issue", "system:test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	post := func(key, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "http://aegispxe.test/api/v1/installations/"+spec.ID+"/telemetry/logs", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+issued.Secret)
		req.Header.Set("Idempotency-Key", key)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	rec := post("log-1", `{"sequence":1,"source":"installer","content":"partitioning started\\ntoken=do-not-store\\npartitioning continued"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("log status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "do-not-store") {
		t.Fatalf("telemetry API reflected sensitive log content: %s", rec.Body.String())
	}

	rec = post("log-3", `{"sequence":3,"source":"installer","content":"gap"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), fault.InstallerTelemetryConflict) {
		t.Fatalf("gap status=%d body=%s", rec.Code, rec.Body.String())
	}

	chunks, err := state.InstallationLogChunks(ctx, spec.ID, 10)
	if err != nil || len(chunks) != 1 {
		t.Fatalf("chunks=%+v err=%v", chunks, err)
	}
	if strings.Contains(chunks[0].Content, "do-not-store") || !strings.Contains(chunks[0].Content, "[REDACTED") {
		t.Fatalf("stored log was not redacted: %q", chunks[0].Content)
	}
}
