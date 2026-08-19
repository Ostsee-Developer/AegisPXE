package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/assignment"
	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
)

func TestPreseedConsumesOneShotBootHandoffAndNextDiscoveryIsLocal(t *testing.T) {
	state, handler := testServer(t)
	const mac = "52:54:00:40:00:55"
	machineRecord, spec := createArmedProvisioningState(t, state, mac)

	req := httptest.NewRequest(http.MethodGet, "http://aegispxe.test/boot/installations/"+spec.ID+"/preseed.cfg", nil)
	req.Header.Set("X-Request-ID", "req_handoff")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("preseed status=%d body=%s", rec.Code, rec.Body.String())
	}

	current, err := state.AssignmentForInstallation(context.Background(), spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != assignment.StateConsumed || current.ConsumedAt.IsZero() {
		t.Fatalf("preseed handoff did not consume assignment: %+v", current)
	}

	events, err := state.Events(context.Background(), event.EntityInstallation, spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[2].Type != event.InstallationAssignmentConsumed {
		t.Fatalf("unexpected installation events after handoff: %+v", events)
	}
	if strings.Contains(strings.ToUpper(events[2].Message), "SUCCESS") || strings.Contains(strings.ToUpper(events[2].Message), "INSTALLER_STARTED") {
		t.Fatalf("boot handoff event must not invent installer lifecycle success: %+v", events[2])
	}

	form := url.Values{
		"mac":          {mac},
		"architecture": {"x86_64"},
		"firmware":     {"efi"},
	}
	req = httptest.NewRequest(http.MethodGet, "http://aegispxe.test/api/v1/discovery.ipxe?"+form.Encode(), nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("post-handoff discovery status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "/boot/installations/"+spec.ID+"/boot.ipxe") {
		t.Fatalf("consumed assignment chained the destructive installer again: %s", body)
	}
	if !strings.Contains(body, "Decision: installation_not_armed") {
		t.Fatalf("post-handoff discovery did not fall back to local boot: %s", body)
	}

	active, err := state.ActiveAssignmentForMachine(context.Background(), machineRecord.ID)
	if fault.Code(err) != fault.InstallationAssignmentNotFound {
		t.Fatalf("expected no active assignment after handoff: %+v err=%v", active, err)
	}
}
