package httpapi

import (
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
)

const maxBootTrustBody = 16 << 10

func (s *Server) registerBootTrust(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/installations/{id}/trust/enroll", s.bootTrustEnroll)
	mux.HandleFunc("GET /api/v1/installations/{id}/trust/status", s.bootTrustStatus)
	mux.HandleFunc("POST /api/v1/installations/{id}/trust/challenge", s.bootTrustChallenge)
	mux.HandleFunc("POST /api/v1/installations/{id}/trust/prove", s.bootTrustProve)
}

type bootTrustEnrollRequest struct {
	PublicKeyPEM  string `json:"public_key_pem"`
	EKFingerprint string `json:"ek_fingerprint,omitempty"`
}

type bootTrustChallengeRequest struct {
	Fingerprint string `json:"fingerprint"`
}

type bootTrustProveRequest struct {
	ChallengeID string `json:"challenge_id"`
	Signature   string `json:"signature"`
}

func (s *Server) bootTrustEnroll(w http.ResponseWriter, r *http.Request) {
	installationID := strings.TrimSpace(r.PathValue("id"))
	var payload bootTrustEnrollRequest
	if err := decodeTelemetryJSON(w, r, maxBootTrustBody, &payload); err != nil {
		s.bootTrustRejected(r, installationID, fault.BootTrustKeyInvalid, "invalid_enrollment_body")
		writeAPIError(w, http.StatusBadRequest, fault.BootTrustKeyInvalid, "invalid boot trust enrollment")
		return
	}
	item, created, err := s.state.RegisterBootTrustKey(r.Context(), installationID, payload.PublicKeyPEM, payload.EKFingerprint, requestID(r.Context()))
	if err != nil {
		s.writeBootTrustError(w, r, installationID, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusAccepted
	}
	writeJSON(w, status, map[string]any{
		"installation_id": installationID,
		"machine_id":      item.MachineID,
		"fingerprint":     item.Fingerprint,
		"state":           item.State,
		"created":         created,
	})
}

func (s *Server) bootTrustStatus(w http.ResponseWriter, r *http.Request) {
	installationID := strings.TrimSpace(r.PathValue("id"))
	fingerprint := strings.TrimSpace(r.URL.Query().Get("fingerprint"))
	spec, err := s.state.InstallationSpec(r.Context(), installationID)
	if err != nil {
		s.writeBootTrustError(w, r, installationID, err)
		return
	}
	item, err := s.state.BootTrustKey(r.Context(), spec.MachineID, fingerprint)
	if err != nil {
		if fault.Code(err) == fault.BootTrustEnrollmentRequired {
			writeJSON(w, http.StatusOK, map[string]any{"installation_id": installationID, "machine_id": spec.MachineID, "fingerprint": fingerprint, "state": "not_enrolled"})
			return
		}
		s.writeBootTrustError(w, r, installationID, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installation_id": installationID,
		"machine_id":      spec.MachineID,
		"fingerprint":     item.Fingerprint,
		"state":           item.State,
		"approved_at":     item.ApprovedAt,
	})
}

func (s *Server) bootTrustChallenge(w http.ResponseWriter, r *http.Request) {
	installationID := strings.TrimSpace(r.PathValue("id"))
	var payload bootTrustChallengeRequest
	if err := decodeTelemetryJSON(w, r, maxBootTrustBody, &payload); err != nil {
		s.bootTrustRejected(r, installationID, fault.BootTrustKeyInvalid, "invalid_challenge_body")
		writeAPIError(w, http.StatusBadRequest, fault.BootTrustKeyInvalid, "invalid boot trust challenge request")
		return
	}
	challenge, err := s.state.CreateBootTrustChallenge(r.Context(), installationID, strings.TrimSpace(payload.Fingerprint), requestID(r.Context()))
	if err != nil {
		s.writeBootTrustError(w, r, installationID, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"challenge_id":    challenge.ID,
		"installation_id": challenge.InstallationID,
		"machine_id":      challenge.MachineID,
		"fingerprint":     challenge.KeyFingerprint,
		"nonce":           base64.RawURLEncoding.EncodeToString(challenge.Nonce),
		"expires_at":      challenge.ExpiresAt,
		"proof_format":    "AEGISPXE-BOOT-TRUST-V1",
	})
}

func (s *Server) bootTrustProve(w http.ResponseWriter, r *http.Request) {
	installationID := strings.TrimSpace(r.PathValue("id"))
	var payload bootTrustProveRequest
	if err := decodeTelemetryJSON(w, r, maxBootTrustBody, &payload); err != nil {
		s.bootTrustRejected(r, installationID, fault.BootTrustProofInvalid, "invalid_proof_body")
		writeAPIError(w, http.StatusBadRequest, fault.BootTrustProofInvalid, "invalid boot trust proof")
		return
	}
	signature, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(payload.Signature))
	if err != nil {
		s.bootTrustRejected(r, installationID, fault.BootTrustProofInvalid, "invalid_signature_encoding")
		writeAPIError(w, http.StatusBadRequest, fault.BootTrustProofInvalid, "invalid boot trust signature")
		return
	}
	release, err := s.state.CompleteBootTrustChallenge(r.Context(), installationID, strings.TrimSpace(payload.ChallengeID), signature, requestID(r.Context()))
	if err != nil {
		s.writeBootTrustError(w, r, installationID, err)
		return
	}
	status := http.StatusCreated
	if release.Duplicate {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{
		"challenge_id":       release.ChallengeID,
		"algorithm":          release.Algorithm,
		"ciphertext":         base64.RawURLEncoding.EncodeToString(release.Ciphertext),
		"credential_expires": release.CredentialExpiry,
		"duplicate":          release.Duplicate,
	})
}

func (s *Server) writeBootTrustError(w http.ResponseWriter, r *http.Request, installationID string, err error) {
	code := fault.Code(err)
	status := http.StatusInternalServerError
	switch code {
	case fault.InstallationNotFound, fault.InstallationAssignmentNotFound:
		status = http.StatusNotFound
	case fault.BootTrustKeyInvalid, fault.BootTrustProofInvalid:
		status = http.StatusBadRequest
	case fault.BootTrustEnrollmentRequired:
		status = http.StatusPreconditionRequired
	case fault.BootTrustChallengeExpired:
		status = http.StatusGone
	case fault.BootTrustReplayRejected, fault.InstallerTelemetryConflict:
		status = http.StatusConflict
	case fault.StorageFailure:
		status = http.StatusServiceUnavailable
	}
	if code == "" {
		code = fault.StorageFailure
	}
	s.bootTrustRejected(r, installationID, code, "request_rejected")
	writeAPIError(w, status, code, "boot trust request rejected")
}

func (s *Server) bootTrustRejected(r *http.Request, installationID, code, cause string) {
	s.logger.WarnContext(r.Context(), "boot trust request rejected",
		"component", "boottrust.http",
		"operation", "authorize",
		"request_id", requestID(r.Context()),
		"installation_id", installationID,
		"remote", remoteHost(r.RemoteAddr),
		"error_code", code,
		"result", "rejected",
		"cause", cause,
	)
}
