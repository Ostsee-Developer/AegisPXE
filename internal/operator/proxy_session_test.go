package operator

import (
	"bytes"
	"log/slog"
	"testing"
)

func TestIssueSessionPreservesTrustedActorAndNormalSessionContract(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(key, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	if err != nil {
		t.Fatal(err)
	}

	token, issued, err := manager.IssueSession("proxy:alice@example.test")
	if err != nil {
		t.Fatal(err)
	}
	stored, ok := manager.Session(token)
	if !ok {
		t.Fatal("trusted-boundary session was not stored")
	}
	if issued.Actor != "proxy:alice@example.test" || stored.Actor != issued.Actor {
		t.Fatalf("actor mismatch: issued=%q stored=%q", issued.Actor, stored.Actor)
	}
	if issued.CSRFToken == "" || !manager.ValidateCSRF(stored, issued.CSRFToken) {
		t.Fatal("trusted-boundary session did not receive the normal CSRF contract")
	}
}
