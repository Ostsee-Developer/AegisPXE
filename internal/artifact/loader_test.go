package artifact

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/observability"
)

func TestHTTPLoaderRejectsHashMismatchWithCorrelatedLog(t *testing.T) {
	content := []byte("tampered-kernel")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	var logs bytes.Buffer
	loader := &HTTPLoader{client: server.Client(), logger: observability.New(&logs, slog.LevelDebug)}
	descriptor := Descriptor{
		ID:         "debian13-amd64-netboot-linux",
		Name:       "linux",
		SourceURL:  server.URL + "/linux",
		Version:    "installer-1",
		Digest:     SHA256([]byte("expected-kernel")),
		Size:       int64(len(content)),
		Provenance: "debian:trixie:test",
	}

	_, err := loader.Load(context.Background(), descriptor, "req_artifact", "i_test")
	if fault.Code(err) != fault.ArtifactHashMismatch {
		t.Fatalf("code=%q err=%v", fault.Code(err), err)
	}
	logText := logs.String()
	if !strings.Contains(logText, fault.ArtifactHashMismatch) || !strings.Contains(logText, `"request_id":"req_artifact"`) || !strings.Contains(logText, `"installation_id":"i_test"`) {
		t.Fatalf("artifact mismatch lacks correlated log: %s", logText)
	}
	if strings.Contains(logText, string(content)) {
		t.Fatal("artifact content leaked into log")
	}
}
