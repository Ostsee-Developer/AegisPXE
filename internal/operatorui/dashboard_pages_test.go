package operatorui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/machine"
	"github.com/Ostsee-Developer/AegisPXE/internal/operator"
	"github.com/Ostsee-Developer/AegisPXE/internal/operatoridentity"
)

func testDashboardSession() operator.Session {
	return operator.Session{
		Actor:      "user:test-operator",
		UserID:     "u_test",
		Role:       operatoridentity.RoleOperator,
		AuthMethod: "test+passkey",
		CSRFToken:  "csrf_test",
	}
}

func renderDashboardViewForTest(t *testing.T, view dashboardView) string {
	t.Helper()
	var out bytes.Buffer
	if err := dashboardPageTemplate.Execute(&out, view); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestDashboardMachineListSelectsCurrentPolicy(t *testing.T) {
	body := renderDashboardViewForTest(t, dashboardView{
		Page:    "machines",
		Title:   "Machines",
		Session: testDashboardSession(),
		Machines: []machine.Machine{{
			ID:           "m_test",
			Policy:       machine.PolicyProvision,
			Architecture: "x86_64",
			Firmware:     "efi",
		}},
	})
	if !strings.Contains(body, `<option value="provision" selected>Provision</option>`) {
		t.Fatalf("current PROVISION policy is not selected: %s", body)
	}
	if strings.Contains(body, `<option value="pending" selected>Pending</option>`) {
		t.Fatal("machine policy form would silently submit PENDING for a PROVISION machine")
	}
}

func TestDashboardMachineDetailRendersPersistedActivity(t *testing.T) {
	item := machine.Machine{ID: "m_test", Policy: machine.PolicyProvision, Architecture: "x86_64", Firmware: "efi"}
	body := renderDashboardViewForTest(t, dashboardView{
		Page:    "machines",
		Title:   "Machine",
		Session: testDashboardSession(),
		Machine: &item,
		Events: []event.Event{{
			Type:       event.MachinePolicyChanged,
			OccurredAt: time.Date(2026, 8, 19, 5, 4, 3, 0, time.UTC),
			RequestID:  "req_policy",
			Actor:      "user:test-operator",
			Message:    "machine policy changed to provision",
		}},
	})
	for _, want := range []string{"Machine activity", event.MachinePolicyChanged, "machine policy changed to provision", "req_policy"} {
		if !strings.Contains(body, want) {
			t.Fatalf("machine activity missing %q: %s", want, body)
		}
	}
}

func TestDashboardOperatorMobileNavigationHasSingleLogsItem(t *testing.T) {
	body := renderDashboardViewForTest(t, dashboardView{Page: "dashboard", Title: "Dashboard", Session: testDashboardSession()})
	if got := strings.Count(body, `<span class="nav-icon">≡</span>Logs`); got != 1 {
		t.Fatalf("mobile log navigation items=%d want=1", got)
	}
}

func TestDashboardLogsExposeFilterControl(t *testing.T) {
	body := renderDashboardViewForTest(t, dashboardView{Page: "logs", Title: "Logs", Session: testDashboardSession()})
	for _, want := range []string{"data-log-filter", "data-live-logs", "/ui/logs/export"} {
		if !strings.Contains(body, want) {
			t.Fatalf("logs page missing %q", want)
		}
	}
}
