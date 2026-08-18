package artifact

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
)

const maxServedArtifactBytes = 256 << 20

type Loader interface {
	Load(context.Context, Descriptor, string, string) ([]byte, error)
}

type HTTPLoader struct {
	client *http.Client
	logger *slog.Logger
}

func NewHTTPLoader(logger *slog.Logger) *HTTPLoader {
	return &HTTPLoader{
		client: &http.Client{Timeout: 2 * time.Minute},
		logger: logger,
	}
}

func (l *HTTPLoader) Load(ctx context.Context, descriptor Descriptor, requestID, installationID string) ([]byte, error) {
	started := time.Now()
	if l.logger == nil {
		return nil, fault.New(fault.ArtifactFetchFailed, "artifact loader logger is required", nil)
	}
	if strings.TrimSpace(requestID) == "" || strings.TrimSpace(installationID) == "" {
		return nil, fault.New(fault.ArtifactFetchFailed, "artifact load correlation identifiers are required", nil)
	}
	if err := descriptor.Validate(); err != nil {
		l.logFailure(ctx, descriptor, requestID, installationID, fault.ArtifactFetchFailed, "invalid_descriptor", started)
		return nil, fault.New(fault.ArtifactFetchFailed, "artifact descriptor is invalid", err)
	}
	if descriptor.Size > maxServedArtifactBytes {
		l.logFailure(ctx, descriptor, requestID, installationID, fault.ArtifactFetchFailed, "size_limit", started)
		return nil, fault.New(fault.ArtifactFetchFailed, "artifact exceeds serving size limit", nil)
	}

	source, err := url.Parse(descriptor.SourceURL)
	if err != nil {
		l.logFailure(ctx, descriptor, requestID, installationID, fault.ArtifactFetchFailed, "invalid_source", started)
		return nil, fault.New(fault.ArtifactFetchFailed, "artifact source URL is invalid", err)
	}
	client := *l.client
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many artifact redirects")
		}
		if req.URL.Scheme != source.Scheme || req.URL.Host != source.Host {
			return errors.New("artifact redirect left pinned origin")
		}
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, descriptor.SourceURL, nil)
	if err != nil {
		l.logFailure(ctx, descriptor, requestID, installationID, fault.ArtifactFetchFailed, "request_build", started)
		return nil, fault.New(fault.ArtifactFetchFailed, "could not build artifact request", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		l.logFailure(ctx, descriptor, requestID, installationID, fault.ArtifactFetchFailed, "fetch", started)
		return nil, fault.New(fault.ArtifactFetchFailed, "could not fetch pinned artifact", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		l.logFailure(ctx, descriptor, requestID, installationID, fault.ArtifactFetchFailed, "http_status", started)
		return nil, fault.New(fault.ArtifactFetchFailed, "pinned artifact source returned an unexpected HTTP status", fmt.Errorf("status %d", resp.StatusCode))
	}

	content, err := io.ReadAll(io.LimitReader(resp.Body, descriptor.Size+1))
	if err != nil {
		l.logFailure(ctx, descriptor, requestID, installationID, fault.ArtifactFetchFailed, "read", started)
		return nil, fault.New(fault.ArtifactFetchFailed, "could not read pinned artifact", err)
	}
	if int64(len(content)) != descriptor.Size {
		l.logFailure(ctx, descriptor, requestID, installationID, fault.ArtifactHashMismatch, "size_mismatch", started)
		return nil, fault.New(fault.ArtifactHashMismatch, "artifact size does not match pinned descriptor", nil)
	}
	if err := VerifyContent(descriptor, content); err != nil {
		l.logFailure(ctx, descriptor, requestID, installationID, fault.ArtifactHashMismatch, "hash_mismatch", started)
		return nil, fault.New(fault.ArtifactHashMismatch, "artifact content does not match pinned digest", err)
	}

	l.logger.InfoContext(ctx, "artifact fetched and verified", "component", "artifact.loader", "operation", "fetch_verified", "request_id", requestID, "installation_id", installationID, "artifact_id", descriptor.ID, "artifact_name", descriptor.Name, "artifact_digest", descriptor.Digest, "artifact_bytes", len(content), "result", "success", "duration_ms", time.Since(started).Milliseconds())
	return content, nil
}

func (l *HTTPLoader) logFailure(ctx context.Context, descriptor Descriptor, requestID, installationID, code, result string, started time.Time) {
	l.logger.WarnContext(ctx, "artifact fetch or verification failed", "component", "artifact.loader", "operation", "fetch_verified", "request_id", requestID, "installation_id", installationID, "artifact_id", descriptor.ID, "artifact_name", descriptor.Name, "artifact_digest", descriptor.Digest, "result", result, "error_code", code, "duration_ms", time.Since(started).Milliseconds())
}
