package telemetryauth

import "testing"

func TestTelemetryHMACTamperResistance(t *testing.T) {
	key := KeyFromSecret("installation-secret")
	body := []byte(`{"stage":"INSTALLER_STARTED"}`)
	canonical, err := Canonical("POST", "/api/v1/installations/i_test/reporter/events", "event-1", 1724054400, body)
	if err != nil {
		t.Fatal(err)
	}
	signature := Sign(key[:], canonical)
	if !Verify(key[:], canonical, signature) {
		t.Fatal("valid telemetry MAC was rejected")
	}
	cases := []struct {
		name string
		path string
		id   string
		time int64
		body []byte
	}{
		{"path", "/api/v1/installations/i_other/reporter/events", "event-1", 1724054400, body},
		{"idempotency", "/api/v1/installations/i_test/reporter/events", "event-2", 1724054400, body},
		{"timestamp", "/api/v1/installations/i_test/reporter/events", "event-1", 1724054401, body},
		{"body", "/api/v1/installations/i_test/reporter/events", "event-1", 1724054400, []byte(`{"stage":"FAILED"}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changed, err := Canonical("POST", tc.path, tc.id, tc.time, tc.body)
			if err != nil {
				t.Fatal(err)
			}
			if Verify(key[:], changed, signature) {
				t.Fatal("tampered telemetry request retained a valid MAC")
			}
		})
	}
}
