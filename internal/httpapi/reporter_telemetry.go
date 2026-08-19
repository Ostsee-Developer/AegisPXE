package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/lifecycle"
	"github.com/Ostsee-Developer/AegisPXE/internal/telemetry"
	"github.com/Ostsee-Developer/AegisPXE/internal/telemetryauth"
)

func (s *Server) registerReporterTelemetry(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/installations/{id}/reporter/events", s.reporterTelemetryEvent)
	mux.HandleFunc("POST /api/v1/installations/{id}/reporter/logs", s.reporterTelemetryLog)
}

func (s *Server) reporterTelemetryEvent(w http.ResponseWriter, r *http.Request) {
	installationID := strings.TrimSpace(r.PathValue("id"))
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if err := lifecycle.ValidateIdempotencyKey(idempotencyKey); err != nil {
		writeAPIError(w, http.StatusBadRequest, fault.InstallerTelemetryInvalid, "valid Idempotency-Key header is required")
		return
	}
	body, ok := s.authenticateReporterBody(w, r, installationID, idempotencyKey, maxTelemetryEventBody)
	if !ok {
		return
	}
	var payload telemetryEventRequest
	if err := decodeStrictBytes(body, &payload); err != nil {
		s.telemetryRejected(r, installationID, fault.InstallerTelemetryInvalid, "invalid_reporter_event_body")
		writeAPIError(w, http.StatusBadRequest, fault.InstallerTelemetryInvalid, "invalid reporter event body")
		return
	}
	clientTime, err := parseOptionalClientTime(payload.ClientTime)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, fault.InstallerTelemetryInvalid, "client_time must be RFC3339 when supplied")
		return
	}
	accepted, duplicate, err := s.state.AppendLifecycleEvent(r.Context(), lifecycle.Report{
		InstallationID: installationID,
		Stage:          payload.Stage,
		Source:         payload.Source,
		ClientAt:       clientTime,
		IdempotencyKey: idempotencyKey,
		Message:        strings.TrimSpace(payload.Message),
		ErrorCode:      strings.TrimSpace(payload.ErrorCode),
		Metadata:       payload.Metadata,
	}, requestID(r.Context()))
	if err != nil {
		s.writeTelemetryError(w, r, installationID, err)
		return
	}
	status := http.StatusAccepted
	if duplicate {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"installation_id": installationID, "sequence": accepted.Sequence, "stage": accepted.Stage, "source": accepted.Source, "received_at": accepted.ReceivedAt, "duplicate": duplicate})
}

func (s *Server) reporterTelemetryLog(w http.ResponseWriter, r *http.Request) {
	installationID := strings.TrimSpace(r.PathValue("id"))
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if err := lifecycle.ValidateIdempotencyKey(idempotencyKey); err != nil {
		writeAPIError(w, http.StatusBadRequest, fault.InstallerTelemetryInvalid, "valid Idempotency-Key header is required")
		return
	}
	body, ok := s.authenticateReporterBody(w, r, installationID, idempotencyKey, maxTelemetryLogBody)
	if !ok {
		return
	}
	var payload telemetryLogRequest
	if err := decodeStrictBytes(body, &payload); err != nil {
		s.telemetryRejected(r, installationID, fault.InstallerTelemetryInvalid, "invalid_reporter_log_body")
		writeAPIError(w, http.StatusBadRequest, fault.InstallerTelemetryInvalid, "invalid reporter log body")
		return
	}
	if len(payload.Content) > telemetry.MaxLogChunkBytes {
		writeAPIError(w, http.StatusRequestEntityTooLarge, fault.InstallerTelemetryInvalid, "installer log chunk exceeds size limit")
		return
	}
	clientTime, err := parseOptionalClientTime(payload.ClientTime)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, fault.InstallerTelemetryInvalid, "client_time must be RFC3339 when supplied")
		return
	}
	accepted, duplicate, err := s.state.AppendInstallationLogChunk(r.Context(), telemetry.LogChunk{
		InstallationID: installationID,
		Sequence:       payload.Sequence,
		Source:         payload.Source,
		ClientAt:       clientTime,
		RequestID:      requestID(r.Context()),
		IdempotencyKey: idempotencyKey,
		Content:        payload.Content,
	})
	if err != nil {
		s.writeTelemetryError(w, r, installationID, err)
		return
	}
	status := http.StatusAccepted
	if duplicate {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"installation_id": installationID, "sequence": accepted.Sequence, "source": accepted.Source, "received_at": accepted.ReceivedAt, "digest": accepted.Digest, "duplicate": duplicate})
}

func (s *Server) authenticateReporterBody(w http.ResponseWriter, r *http.Request, installationID, idempotencyKey string, limit int64) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.telemetryRejected(r, installationID, fault.InstallerTelemetryInvalid, "reporter_body_too_large_or_unreadable")
		writeAPIError(w, http.StatusBadRequest, fault.InstallerTelemetryInvalid, "invalid reporter request body")
		return nil, false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	prefix := telemetryauth.Scheme + " "
	if !strings.HasPrefix(authorization, prefix) {
		s.telemetryRejected(r, installationID, fault.InstallerCredentialRequired, "missing_hmac_authorization")
		writeAPIError(w, http.StatusUnauthorized, fault.InstallerCredentialRequired, "HMAC installation authentication required")
		return nil, false
	}
	timestamp, err := strconv.ParseInt(strings.TrimSpace(r.Header.Get("X-Aegis-Timestamp")), 10, 64)
	if err != nil || timestamp <= 0 {
		s.telemetryRejected(r, installationID, fault.InstallerCredentialInvalid, "invalid_hmac_timestamp")
		writeAPIError(w, http.StatusUnauthorized, fault.InstallerCredentialInvalid, "invalid telemetry authentication timestamp")
		return nil, false
	}
	signature := strings.TrimSpace(strings.TrimPrefix(authorization, prefix))
	if err := s.state.VerifyLifecycleMAC(r.Context(), installationID, r.Method, r.URL.Path, idempotencyKey, timestamp, body, signature); err != nil {
		code := fault.Code(err)
		if code == "" {
			code = fault.InstallerCredentialInvalid
		}
		s.telemetryRejected(r, installationID, code, "hmac_verification_failed")
		writeAPIError(w, http.StatusUnauthorized, code, "reporter telemetry authentication rejected")
		return nil, false
	}
	return body, true
}

func decodeStrictBytes(body []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON object")
	}
	return nil
}
