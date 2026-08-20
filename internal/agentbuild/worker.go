package agentbuild

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/agent"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
)

const defaultPollInterval = 2 * time.Second

type Queue interface {
	ClaimNextAgentBuild(context.Context, string, string) (agent.Record, agent.Build, error)
	CompleteAgentBuild(context.Context, string, string, string, int64, string, string, string) (agent.Build, error)
	FailAgentBuild(context.Context, string, string, string, string) (agent.Build, error)
}

type Worker struct {
	queue    Queue
	builder  *Builder
	version  string
	interval time.Duration
	logger   *slog.Logger
}

func NewWorker(queue Queue, builder *Builder, version string, logger *slog.Logger) (*Worker, error) {
	version = strings.TrimSpace(version)
	if queue == nil || builder == nil || version == "" || len(version) > 128 {
		return nil, errors.New("agent build worker configuration is invalid")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{queue: queue, builder: builder, version: version, interval: defaultPollInterval, logger: logger}, nil
}

func (w *Worker) Run(ctx context.Context) error {
	if w == nil {
		return errors.New("agent build worker is unavailable")
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			worked, err := w.ProcessOne(ctx)
			if err != nil {
				w.logger.ErrorContext(ctx, "managed agent build worker iteration failed",
					"component", "agent.build_worker",
					"operation", "process_one",
					"error_code", fault.Code(err),
					"error", err,
					"result", "failure",
				)
			}
			delay := w.interval
			if worked {
				delay = 0
			}
			timer.Reset(delay)
		}
	}
}

func (w *Worker) ProcessOne(ctx context.Context) (bool, error) {
	requestID, err := idgen.New("req_")
	if err != nil {
		return false, fault.New(fault.StorageFailure, "could not allocate agent build request identifier", err)
	}
	record, build, err := w.queue.ClaimNextAgentBuild(ctx, w.version, requestID)
	if err != nil {
		if fault.Code(err) == fault.AgentBuildQueueEmpty {
			return false, nil
		}
		return false, err
	}
	artifact, buildErr := w.builder.Build(ctx, record, build)
	if buildErr != nil {
		message := boundedBuildError(buildErr)
		if _, failErr := w.queue.FailAgentBuild(ctx, build.ID, fault.AgentBuildFailed, message, requestID); failErr != nil {
			return true, errors.Join(buildErr, failErr)
		}
		return true, fault.New(fault.AgentBuildFailed, "managed agent build failed", buildErr)
	}
	if _, err := w.queue.CompleteAgentBuild(ctx, build.ID, artifact.PackagePath, artifact.PackageSHA256, artifact.PackageSize, artifact.ManifestSHA256, artifact.ManifestSignature, requestID); err != nil {
		return true, err
	}
	return true, nil
}

func boundedBuildError(err error) string {
	if err == nil {
		return "managed agent build failed"
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "managed agent build failed"
	}
	const limit = 480
	if len(message) > limit {
		message = message[:limit]
	}
	return message
}
