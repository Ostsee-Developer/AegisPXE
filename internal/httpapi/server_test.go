package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
	"github.com/Ostsee-Developer/AegisPXE/internal/store"
)

func TestRepeatedDiscoveryKeepsOnePendingMachine(t *testing.T) {
	state, handler := testServer(t)

	form := url.Values{
		"mac":          {"52:54:00:12:34:56"},
		"smbios_uuid":  {"11111111-2222-3333-4444-555555555555"},
		"architecture": {"x86_64"},
		"firmware":     {"efi"},
	}

	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodGet, "http://aegispxe.test/api/v1/discovery.ipxe?"+form.Encode(), nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("discovery %d status = %d, body = %s", i+1, rec.Code, rec.Body.String())
		}
	}

	machines, err := state.Machines(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(machines) != 1 {
		t.Fatalf("machines = %d, want 1", len(machines))
	}
	if machines[0].Policy != machine.PolicyPending {
		t.Fatalf("policy = %s, want pending", machines[0].Policy)
	}

	events, err := state.Events(context.Background(), event.EntityMachine, machines[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 20 {
		t.Fatalf("events = %d, want 20", len(events))
	}
	if events[0].Type != event.MachineDiscovered {
		t.Fatalf("first event = %s, want %s", events[0].Type, event.MachineDiscovered)
	}
	for _, item := range events[1:] {
		if item.Type != event.MachineSeen {
			t.Fatalf("repeat event = %s, want %s", item.Type, event.MachineSeen)
		}
	}
}

func TestDashboardShowsPendingMachine(t *testing.T) {
	_, handler := testServer(t)
	form := url.Values{"mac": {"52:54:00:12:34:57"}}
	req := httptest.NewRequest(http.MethodPost, "http://aegispxe.test/api/v1/discovery.ipxe", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("discovery status = %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "http://aegispxe.test/ui/", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("web ui status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "PENDING") || !strings.Contains(rec.Body.String(), "Discovery inventory") {
		t.Fatalf("web ui does not show pending discovery inventory: %s", rec.Body.String())
	}
}

func TestProvisionPolicyFailsClosedWithoutInstallationSpec(t *testing.T) {
	state, handler := testServer(t)

	payload := []byte(`{"mac":"52:54:00:aa:bb:cc","smbios_uuid":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee","architecture":"x86_64","firmware":"efi"}`)
	req := httptest.NewRequest(http.MethodPost, "http://aegispxe.test/api/v1/discovery", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("initial discovery status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var initial discoveryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}

	if _, err := state.SetMachinePolicy(context.Background(), initial.MachineID, machine.PolicyProvision, "req_test_policy", "test:operator"); err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(http.MethodPost, "http://aegispxe.test/api/v1/discovery", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("repeat discovery status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var decision discoveryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &decision); err != nil {
		t.Fatal(err)
	}
	if decision.Action != "local" || decision.Reason != "installation_not_armed" {
		t.Fatalf("decision = %+v, want local installation_not_armed", decision)
	}
}

func TestDiscoveryRateLimitIsBounded(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	server := New(nil, logger, "test")
	req := httptest.NewRequest(http.MethodPost, "http://aegispxe.test/api/v1/discovery", nil)
	req.RemoteAddr = "192.0.2.25:40123"
	for i := 0; i < maxDiscoveryPerWindow; i++ {
		if !server.allowDiscovery(req) {
			t.Fatalf("request %d was rate limited too early", i+1)
		}
	}
	if server.allowDiscovery(req) {
		t.Fatal("request beyond discovery window limit was allowed")
	}
}

func TestBootstrapUsesStockIPXEQueryParameters(t *testing.T) {
	_, handler := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "http://192.0.2.10:8090/boot/discovery.ipxe", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bootstrap status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, expected := range []string{
		"#!ipxe",
		"chain http://192.0.2.10:8090/api/v1/discovery.ipxe?mac=${net0/mac}&smbios_uuid=${uuid:uristring}&architecture=${buildarch:uristring}&firmware=${platform:uristring} || goto discovery_failed\nexit 0\n:discovery_failed",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("bootstrap missing %q: %s", expected, body)
		}
	}
	if strings.Contains(body, "\nparams\n") || strings.Contains(body, "\nparam ") || strings.Contains(body, "##params") {
		t.Fatalf("bootstrap unexpectedly requires PARAM_CMD: %s", body)
	}
}

func testServer(t *testing.T) (*store.Store, http.Handler) {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	state, err := store.Open(context.Background(), t.TempDir()+"/aegispxe.db", logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Close() })
	return state, New(state, logger, "test-version").Handler()
}
