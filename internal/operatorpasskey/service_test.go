package operatorpasskey

import (
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

func TestPasskeyCeremonyStoreIsBounded(t *testing.T) {
	now := time.Date(2026, 8, 19, 16, 0, 0, 0, time.UTC)
	service := &Service{flows: make(map[[32]byte]flow), now: func() time.Time { return now }}
	for index := 0; index < maxActiveFlows; index++ {
		if _, err := service.saveFlow("u_test", ModeLogin, webauthn.SessionData{}); err != nil {
			t.Fatalf("flow %d: %v", index, err)
		}
	}
	if _, err := service.saveFlow("u_test", ModeLogin, webauthn.SessionData{}); err == nil {
		t.Fatal("ceremony store accepted an entry beyond its configured bound")
	}

	now = now.Add(flowLifetime + time.Second)
	if _, err := service.saveFlow("u_test", ModeLogin, webauthn.SessionData{}); err != nil {
		t.Fatalf("expired ceremonies were not reclaimed: %v", err)
	}
	if len(service.flows) != 1 {
		t.Fatalf("active ceremonies after expiry cleanup=%d want=1", len(service.flows))
	}
}
