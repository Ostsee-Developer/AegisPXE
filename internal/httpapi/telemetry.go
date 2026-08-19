package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/lifecycle"
	"github.com/Ostsee-Developer/AegisPXE/internal/telemetry"
)

const (
	maxTelemetryEventBody = 32 << 10
	maxTelemetryLogBody   = telemetry.MaxLogChunkBytes + (32 << 10)
)

func (s *Server) registerTelemetry(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/installations/{id}/telemetry/events", s.installationTelemetryEvent)
	mux.HandleFunc("POST /api/v1/installations/{id}/telemetry/logs", s.installationTelemetryLog)
}

type telemetryEventRequest struct {
	Stage      lifecycle.Stage     `json:"stage"`
	Source     lifecycle.Source    `json:"source"`
	ClientTime string              `json:"client_time,omitempty"`
	Message    string              `json:"message,omitempty"`
	ErrorCode  string              `json:"error_code,omitempty"`
	Metadata   map[string]string   `json:"metadata,omitempty"`
}

type telemetryLogRequest struct {
	Sequence   int64            `json:"sequence"`
	Source     lifecycle.Source `json:"source"`
	ClientTime string           `json:"client_time,omitempty"`
	Content    string           `json:"content"`
}

func (s *Server) installationTelemetryEvent(w http.ResponseWriter, r *http.Request) {
	installationID := strings.TrimSpace(r.PathValue("id"))
	if !s.authenticateInstallerTelemetry(w, r, installationID) {
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if err := lifecycle.ValidateIdempotencyKey(idempotencyKey); err != nil {
		s.telemetryRejected(r, installationID, fault.InstallerTelemetryInvalid, "invalid_idempotency_key")
		writeAPIError(w, http.StatusBadRequest, fault.InstallerTelemetryInvalid, "valid Idempotency-Key header is required")
		return
	}

	var payload telemetryEventRequest
	if err := decodeTelemetryJSON(w, r, maxTelemetryEventBody, &payload); err != nil {
		s.telemetryRejected(r, installationID, fault.InstallerTelemetryInvalid, "invalid_event_body")
		writeAPIError(w, http.StatusBadRequest, fault.InstallerTelemetryInvalid, "invalid telemetry event body")
		return
	}
	clientTime, err := parseOptionalClientTime(payload.ClientTime)
	if err != nil {
		s.telemetryRejected(r, installationID, fault.InstallerTelemetryInvalid, "invalid_client_time")
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
	writeJSON(w, status, map[string]any{
		"installation_id": installationID,
		"sequence":        accepted.Sequence,
		"stage":           accepted.Stage,
		"source":          accepted.Source,
		"received_at":     accepted.ReceivedAt,
		"duplicate":       duplicate,
	})
}

func (s *Server) installationTelemetryLog(w http.ResponseWriter, r *http.Request) {
	installationID := strings.TrimSpace(r.PathValue("id"))
	if !s.authenticateInstallerTelemetry(w, r, installationID) {
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if err := lifecycle.ValidateIdempotencyKey(idempotencyKey); err != nil {
		s.telemetryRejected(r, installationID, fault.InstallerTelemetryInvalid, "invalid_idempotency_key")
		writeAPIError(w, http.StatusBadRequest, fault.InstallerTelemetryInvalid, "valid Idempotency-Key header is required")
		return
	}

	var payload telemetryLogRequest
	if err := decodeTelemetryJSON(w, r, maxTelemetryLogBody, &payload); err != nil {
		s.telemetryRejected(r, installationID, fault.InstallerTelemetryInvalid, "invalid_log_body")
		writeAPIError(w, http.StatusBadRequest, fault.InstallerTelemetryInvalid, "invalid telemetry log body")
		return
	}
	if len(payload.Content) > telemetry.MaxLogChunkBytes {
		s.telemetryRejected(r, installationID, fault.InstallerTelemetryInvalid, "log_chunk_too_large")
		writeAPIError(w, http.StatusRequestEntityTooLarge, fault.InstallerTelemetryInvalid, "installer log chunk exceeds size limit")
		return
	}
	clientTime, err := parseOptionalClientTime(payload.ClientTime)
	if err != nil {
		s.telemetryRejected(r, installationID, fault.InstallerTelemetryInvalid, "invalid_client_time")
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
	writeJSON(w, status, map[string]any{
		"installation_id": installationID,
		"sequence":        accepted.Sequence,
		"source":          accepted.Source,
		"received_at":     accepted.ReceivedAt,
		"digest":          accepted.Digest,
		"duplicate":       duplicate,
	})
}

func (s *Server) authenticateInstallerTelemetry(w http.ResponseWriter, r *http.Request, installationID string) bool {
	if installationID == "" {
		s.telemetryRejected(r, installationID, fault.InstallationNotFound, "missing_installation_id")
		writeAPIError(w, http.StatusNotFound, fault.InstallationNotFound, "installation not found")
		return false
	}
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if !strings.HasPrefix(authorization, prefix) || strings.TrimSpace(strings.TrimPrefix(authorization, prefix)) == "" {
		s.telemetryRejected(r, installationID, fault.InstallerCredentialRequired, "missing_bearer_credential")
		w.Header().Set("WWW-Authenticate", `Bearer realm="aegispxe-installer"`)
		writeAPIError(w, http.StatusUnauthorized, fault.InstallerCredentialRequired, "installation-scoped credential required")
		return false
	}
	secret := strings.TrimSpace(strings.TrimPrefix(authorization, prefix))
	if _, err := s.state.AuthenticateLifecycleCredential(r.Context(), installationID, secret); err != nil {
		code := fault.Code(err)
		if code == "" {
			code = fault.InstallerCredentialInvalid
		}
		s.telemetryRejected(r, installationID, code, "credential_rejected")
		w.Header().Set("WWW-Authenticate", `Bearer realm="aegispxe-installer"`)
		writeAPIError(w, http.StatusUnauthorized, code, "installation-scoped credential rejected")
		return false
	}
	return true
}

func (s *Server) writeTelemetryError(w http.ResponseWriter, r *http.Request, installationID string, err error) {
	code := fault.Code(err)
	status := http.StatusInternalServerError
	switch code {
	case fault.InstallationNotFound:
		status = http.StatusNotFound
	case fault.InstallerTelemetryInvalid:
		status = http.StatusBadRequest
	case fault.InstallerTelemetryConflict:
		status = http.StatusConflict
	case fault.InstallerLogLimitExceeded:
		status = http.StatusRequestEntityTooLarge
	case fault.InstallerCredentialRequired, fault.InstallerCredentialInvalid, fault.InstallerCredentialExpired:
		status = http.StatusUnauthorized
	case fault.StorageFailure:
		status = http.StatusServiceUnavailable
	}
	if code == "" {
		code = fault.StorageFailure
	}
	s.telemetryRejected(r, installationID, code, "ingestion_failed")
	writeAPIError(w, status, code, "installer telemetry request rejected")
}

func (s *Server) telemetryRejected(r *http.Request, installationID, code, cause string) {
	s.logger.WarnContext(r.Context(), "installer telemetry rejected",
		"component", "installer.telemetry",
		"operation", "ingest",
		"request_id", requestID(r.Context()),
		"installation_id", installationID,
		"remote", remoteHost(r.RemoteAddr),
		"error_code", code,
		"result", "rejected",
		"cause", cause,
	)
}

func decodeTelemetryJSON(w http.ResponseWriter, r *http.Request, limit int64, target any) error {
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if contentType != "application/json" {
		return errors.New("content type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request must contain exactly one JSON object")
	}
	return nil
}

func parseOptionalClientTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}
