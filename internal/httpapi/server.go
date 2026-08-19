package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/boot"
	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
	"github.com/Ostsee-Developer/AegisPXE/internal/lifecycle"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
	"github.com/Ostsee-Developer/AegisPXE/internal/store"
)

const (
	maxDiscoveryBody      = 8 << 10
	discoveryWindow       = time.Minute
	maxDiscoveryPerWindow = 120
)

type Server struct {
	state   *store.Store
	logger  *slog.Logger
	version string
	limiter discoveryLimiter
}

type discoveryLimiter struct {
	mu      sync.Mutex
	clients map[string]discoveryCounter
}

type discoveryCounter struct {
	started time.Time
	count   int
}

type discoveryResponse struct {
	MachineID      string         `json:"machine_id"`
	Created        bool           `json:"created"`
	Policy         machine.Policy `json:"policy"`
	Action         boot.Action    `json:"action"`
	Reason         string         `json:"reason"`
	InstallationID string         `json:"installation_id,omitempty"`
	AssignmentID   string         `json:"-"`
}

type identifierResponse struct {
	Kind  machine.IdentifierKind `json:"kind"`
	Value string                 `json:"value"`
}

type machineResponse struct {
	ID           string               `json:"id"`
	Policy       machine.Policy       `json:"policy"`
	Architecture string               `json:"architecture"`
	Firmware     string               `json:"firmware"`
	FirstSeen    time.Time            `json:"first_seen"`
	LastSeen     time.Time            `json:"last_seen"`
	Identifiers  []identifierResponse `json:"identifiers,omitempty"`
}

type eventResponse struct {
	Sequence   int64     `json:"sequence"`
	Type       string    `json:"type"`
	OccurredAt time.Time `json:"occurred_at"`
	RequestID  string    `json:"request_id,omitempty"`
	Actor      string    `json:"actor,omitempty"`
	Message    string    `json:"message,omitempty"`
	ErrorCode  string    `json:"error_code,omitempty"`
}

type requestIDKey struct{}

func New(state *store.Store, logger *slog.Logger, version string) *Server {
	return &Server{state: state, logger: logger, version: version, limiter: discoveryLimiter{clients: make(map[string]discoveryCounter)}}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /boot/discovery.ipxe", s.discoveryBootstrap)
	mux.HandleFunc("POST /api/v1/discovery", s.discoveryJSON)
	mux.HandleFunc("GET /api/v1/discovery.ipxe", s.discoveryIPXE)
	mux.HandleFunc("POST /api/v1/discovery.ipxe", s.discoveryIPXE)
	mux.HandleFunc("GET /api/v1/machines", s.machines)
	mux.HandleFunc("GET /api/v1/machines/{id}", s.machine)
	mux.HandleFunc("GET /api/v1/machines/{id}/events", s.machineEvents)
	s.registerProvisioning(mux)
	s.registerTelemetry(mux)
	return requestLog(s.logger, mux)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.state.Ping(r.Context()); err != nil {
		s.logger.ErrorContext(r.Context(), "health check failed", "component", "httpapi", "operation", "health", "error_code", fault.StorageFailure, "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.version})
}

func (s *Server) discoveryJSON(w http.ResponseWriter, r *http.Request) {
	if !s.allowDiscovery(r) {
		w.Header().Set("Retry-After", "60")
		s.logger.WarnContext(r.Context(), "machine discovery rate limited", "component", "httpapi", "operation", "discover", "request_id", requestID(r.Context()), "error_code", fault.DiscoveryRateLimited, "remote", remoteHost(r.RemoteAddr))
		writeAPIError(w, http.StatusTooManyRequests, fault.DiscoveryRateLimited, "discovery rate limit exceeded")
		return
	}
	observation, err := decodeObservation(w, r)
	if err != nil {
		s.logger.WarnContext(r.Context(), "machine discovery input rejected", "component", "httpapi", "operation", "discover", "request_id", requestID(r.Context()), "error_code", fault.MachineIdentityInvalid, "error", err)
		writeAPIError(w, http.StatusBadRequest, fault.MachineIdentityInvalid, err.Error())
		return
	}
	response, err := s.discover(r.Context(), observation, requestID(r.Context()))
	if err != nil {
		s.writeDiscoveryError(w, r, err)
		return
	}
	status := http.StatusOK
	if response.Created {
		status = http.StatusCreated
	}
	writeJSON(w, status, response)
}

func (s *Server) discoveryIPXE(w http.ResponseWriter, r *http.Request) {
	if !s.allowDiscovery(r) {
		s.logger.WarnContext(r.Context(), "iPXE discovery rate limited", "component", "httpapi", "operation", "discover_ipxe", "request_id", requestID(r.Context()), "error_code", fault.DiscoveryRateLimited, "remote", remoteHost(r.RemoteAddr))
		s.writeIPXE(w, "Discovery rate limit reached; exiting safely", "rate_limited")
		return
	}
	observation, err := decodeObservation(w, r)
	if err != nil {
		s.logger.WarnContext(r.Context(), "iPXE discovery input rejected", "component", "httpapi", "operation", "discover_ipxe", "request_id", requestID(r.Context()), "error_code", fault.MachineIdentityInvalid, "error", err)
		s.writeIPXE(w, "Discovery request rejected", "invalid_observation")
		return
	}
	response, err := s.discover(r.Context(), observation, requestID(r.Context()))
	if err != nil {
		s.logger.WarnContext(r.Context(), "iPXE discovery failed", "component", "httpapi", "operation", "discover_ipxe", "request_id", requestID(r.Context()), "error_code", fault.Code(err), "error", err)
		s.writeIPXE(w, "Discovery failed safely", "server_rejected")
		return
	}
	if response.Action == boot.ActionProvision && response.InstallationID != "" {
		if _, _, err := s.state.RecordServerLifecycle(r.Context(), response.InstallationID, lifecycle.StagePXEBooted,
			"server:pxe_booted:"+response.InstallationID, "PXE bootloader checked in for armed installation", requestID(r.Context())); err != nil {
			s.logger.WarnContext(r.Context(), "PXE lifecycle check-in rejected",
				"component", "installer.telemetry",
				"operation", "pxe_checkin",
				"request_id", requestID(r.Context()),
				"machine_id", response.MachineID,
				"installation_id", response.InstallationID,
				"assignment_id", response.AssignmentID,
				"error_code", fault.Code(err),
				"result", "rejected",
				"cause", err.Error(),
			)
			s.writeIPXE(w, "Provisioning lifecycle state rejected safely", "server_rejected")
			return
		}
		s.writeProvisioningChain(w, r, response.InstallationID)
		return
	}

	message := fmt.Sprintf("Machine %s registered (%s)", response.MachineID, response.Policy)
	if response.Action == boot.ActionBlocked {
		message = fmt.Sprintf("Machine %s is blocked by AegisPXE policy", response.MachineID)
	}
	s.writeIPXE(w, message, response.Reason)
}

func (s *Server) discover(ctx context.Context, observation machine.Observation, requestID string) (discoveryResponse, error) {
	item, created, err := s.state.DiscoverMachine(ctx, observation, requestID)
	if err != nil {
		return discoveryResponse{}, err
	}
	decision, installationID, assignmentID, err := s.assignmentDecision(ctx, item, requestID)
	if err != nil {
		return discoveryResponse{}, err
	}
	s.logger.InfoContext(ctx, "boot decision evaluated",
		"component", "boot.policy",
		"operation", "evaluate",
		"request_id", requestID,
		"machine_id", item.ID,
		"installation_id", installationID,
		"assignment_id", assignmentID,
		"policy", item.Policy,
		"action", decision.Action,
		"reason", decision.Reason,
		"result", "success",
	)
	return discoveryResponse{
		MachineID:      item.ID,
		Created:        created,
		Policy:         item.Policy,
		Action:         decision.Action,
		Reason:         decision.Reason,
		InstallationID: installationID,
		AssignmentID:   assignmentID,
	}, nil
}

func (s *Server) discoveryBootstrap(w http.ResponseWriter, r *http.Request) {
	base := requestBaseURL(r)
	endpoint := base + "/api/v1/discovery.ipxe?mac=${net0/mac}&smbios_uuid=${uuid}&architecture=${buildarch:uristring}&firmware=${platform:uristring}"
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintf(w, "#!ipxe\n")
	_, _ = fmt.Fprintf(w, "echo AegisPXE headless discovery\n")
	_, _ = fmt.Fprintf(w, "chain %s || goto discovery_failed\n", endpoint)
	_, _ = fmt.Fprintf(w, "exit 0\n")
	_, _ = fmt.Fprintf(w, ":discovery_failed\n")
	_, _ = fmt.Fprintf(w, "echo AegisPXE discovery endpoint unavailable; exiting safely\n")
	_, _ = fmt.Fprintf(w, "exit 0\n")
}

func (s *Server) machines(w http.ResponseWriter, r *http.Request) {
	items, err := s.state.Machines(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, fault.StorageFailure, "could not load machines")
		return
	}
	out := make([]machineResponse, 0, len(items))
	for _, item := range items {
		out = append(out, machineView(item, nil))
	}
	writeJSON(w, http.StatusOK, map[string]any{"machines": out})
}

func (s *Server) machine(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	item, err := s.state.Machine(r.Context(), id)
	if err != nil {
		s.writeMachineReadError(w, err)
		return
	}
	identifiers, err := s.state.MachineIdentifiers(r.Context(), id)
	if err != nil {
		s.writeMachineReadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, machineView(item, identifiers))
}

func (s *Server) machineEvents(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if _, err := s.state.Machine(r.Context(), id); err != nil {
		s.writeMachineReadError(w, err)
		return
	}
	items, err := s.state.Events(r.Context(), event.EntityMachine, id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, fault.StorageFailure, "could not load machine events")
		return
	}
	out := make([]eventResponse, 0, len(items))
	for _, item := range items {
		out = append(out, eventResponse{Sequence: item.Sequence, Type: item.Type, OccurredAt: item.OccurredAt, RequestID: item.RequestID, Actor: item.Actor, Message: item.Message, ErrorCode: item.ErrorCode})
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}

func (s *Server) writeDiscoveryError(w http.ResponseWriter, r *http.Request, err error) {
	code := fault.Code(err)
	status := http.StatusInternalServerError
	switch code {
	case fault.MachineIdentityInvalid:
		status = http.StatusBadRequest
	case fault.MachineIdentityConflict:
		status = http.StatusConflict
	case fault.StorageFailure:
		status = http.StatusServiceUnavailable
	}
	s.logger.WarnContext(r.Context(), "machine discovery request failed", "component", "httpapi", "operation", "discover", "request_id", requestID(r.Context()), "error_code", code, "error", err)
	writeAPIError(w, status, code, err.Error())
}

func (s *Server) writeMachineReadError(w http.ResponseWriter, err error) {
	if fault.Code(err) == fault.MachineNotFound {
		writeAPIError(w, http.StatusNotFound, fault.MachineNotFound, "machine not found")
		return
	}
	writeAPIError(w, http.StatusInternalServerError, fault.StorageFailure, "could not load machine")
}

func (s *Server) writeIPXE(w http.ResponseWriter, message, reason string) {
	if localBootReason(reason) {
		s.writeLocalBootIPXE(w, message, reason)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintf(w, "#!ipxe\n")
	_, _ = fmt.Fprintf(w, "echo %s\n", ipxeSafe(message))
	_, _ = fmt.Fprintf(w, "echo Decision: %s\n", ipxeSafe(reason))
	_, _ = fmt.Fprintf(w, "exit 0\n")
}

func decodeObservation(w http.ResponseWriter, r *http.Request) (machine.Observation, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxDiscoveryBody)
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if contentType == "application/json" {
		var payload struct {
			MAC          string `json:"mac"`
			SMBIOSUUID   string `json:"smbios_uuid"`
			Architecture string `json:"architecture"`
			Firmware     string `json:"firmware"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&payload); err != nil {
			return machine.Observation{}, fmt.Errorf("decode discovery JSON: %w", err)
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return machine.Observation{}, errors.New("discovery request must contain one JSON object")
		}
		return machine.Observation{MAC: payload.MAC, SMBIOSUUID: payload.SMBIOSUUID, Architecture: payload.Architecture, Firmware: payload.Firmware}, nil
	}

	if err := r.ParseForm(); err != nil {
		return machine.Observation{}, fmt.Errorf("decode discovery form: %w", err)
	}
	return machine.Observation{
		MAC:          r.Form.Get("mac"),
		SMBIOSUUID:   firstNonEmpty(r.Form.Get("smbios_uuid"), r.Form.Get("uuid")),
		Architecture: r.Form.Get("architecture"),
		Firmware:     r.Form.Get("firmware"),
	}, nil
}

func machineView(item machine.Machine, identifiers []machine.Identifier) machineResponse {
	out := machineResponse{ID: item.ID, Policy: item.Policy, Architecture: item.Architecture, Firmware: item.Firmware, FirstSeen: item.FirstSeen, LastSeen: item.LastSeen}
	for _, identifier := range identifiers {
		out.Identifiers = append(out.Identifiers, identifierResponse{Kind: identifier.Kind, Value: identifier.Value})
	}
	return out
}

func requestBaseURL(r *http.Request) string {
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return "http://127.0.0.1"
	}
	if parsed, err := url.Parse("http://" + host); err != nil || parsed.Host != host || parsed.User != nil {
		return "http://127.0.0.1"
	}
	return "http://" + host
}

func (s *Server) allowDiscovery(r *http.Request) bool {
	key := remoteHost(r.RemoteAddr)
	now := time.Now()

	s.limiter.mu.Lock()
	defer s.limiter.mu.Unlock()
	if len(s.limiter.clients) >= 1024 {
		for client, counter := range s.limiter.clients {
			if now.Sub(counter.started) >= 2*discoveryWindow {
				delete(s.limiter.clients, client)
			}
		}
	}
	if _, known := s.limiter.clients[key]; !known && len(s.limiter.clients) >= 4096 {
		return false
	}
	counter := s.limiter.clients[key]
	if counter.started.IsZero() || now.Sub(counter.started) >= discoveryWindow {
		counter = discoveryCounter{started: now}
	}
	if counter.count >= maxDiscoveryPerWindow {
		s.limiter.clients[key] = counter
		return false
	}
	counter.count++
	s.limiter.clients[key] = counter
	return true
}

func remoteHost(remoteAddr string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err == nil && host != "" {
		return host
	}
	if value := strings.TrimSpace(remoteAddr); value != "" {
		return value
	}
	return "unknown"
}

func requestLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" || len(requestID) > 128 {
			var err error
			requestID, err = idgen.New("req_")
			if err != nil {
				logger.ErrorContext(r.Context(), "request ID allocation failed", "component", "http", "operation", "request_id", "error_code", fault.StorageFailure, "error", err)
				http.Error(w, "internal request tracking failure", http.StatusInternalServerError)
				return
			}
		}
		ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		started := time.Now()
		next.ServeHTTP(w, r.WithContext(ctx))
		logger.DebugContext(ctx, "http request", "component", "http", "operation", "request", "request_id", requestID, "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds())
	})
}

func requestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey{}).(string)
	return value
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func ipxeSafe(value string) string {
	replacer := strings.NewReplacer("\r", " ", "\n", " ", ";", " ")
	return replacer.Replace(value)
}
