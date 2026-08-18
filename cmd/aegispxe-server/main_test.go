package main

import (
	"net/http"
	"testing"
	"time"
)

func TestOperatorListenerAcceptsOnlyLoopbackIP(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8091", "[::1]:8091"} {
		if err := validateOperatorListen(address); err != nil {
			t.Fatalf("validateOperatorListen(%q) = %v, want success", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:8091", "192.168.100.10:8091", "localhost:8091", ":8091"} {
		if err := validateOperatorListen(address); err == nil {
			t.Fatalf("validateOperatorListen(%q) succeeded, want fail-closed rejection", address)
		}
	}
}

func TestHTTPServerWriteTimeoutCoversFullDebianTrustResolution(t *testing.T) {
	server := newHTTPServer("127.0.0.1:0", http.NotFoundHandler())
	if server.WriteTimeout != 10*time.Minute {
		t.Fatalf("WriteTimeout=%s want=10m", server.WriteTimeout)
	}
}
