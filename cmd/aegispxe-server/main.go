package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/httpapi"
	"github.com/Ostsee-Developer/AegisPXE/internal/observability"
	"github.com/Ostsee-Developer/AegisPXE/internal/operator"
	"github.com/Ostsee-Developer/AegisPXE/internal/operatorui"
	"github.com/Ostsee-Developer/AegisPXE/internal/store"
)

// version is injected from the repository VERSION file by scripts/build.sh.
// The fallback identifies binaries built directly with `go build` as non-release builds.
var version = "dev"

const serverWriteTimeout = 3 * time.Minute

func main() {
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}

	level := new(slog.LevelVar)
	if err := level.UnmarshalText([]byte(env("AEGISPXE_LOG_LEVEL", "info"))); err != nil {
		fmt.Fprintf(os.Stderr, "invalid AEGISPXE_LOG_LEVEL: %v\n", err)
		os.Exit(2)
	}
	logger := observability.New(os.Stdout, level)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	state, err := store.Open(ctx, env("AEGISPXE_DB", "/var/lib/aegispxe/aegispxe.db"), logger)
	if err != nil {
		logger.Error("startup failed", "component", "server", "operation", "open_store", "error_code", fault.StorageFailure, "error", err)
		os.Exit(1)
	}
	defer state.Close()

	operatorAuth, err := operator.LoadOrCreate(env("AEGISPXE_OPERATOR_KEY", "/var/lib/aegispxe/operator.key"), logger)
	if err != nil {
		logger.Error("startup failed", "component", "operator.auth", "operation", "bootstrap_key", "error_code", fault.StorageFailure, "error", err)
		os.Exit(1)
	}

	operatorAddress := env("AEGISPXE_OPERATOR_LISTEN", "127.0.0.1:8091")
	if !strings.EqualFold(operatorAddress, "disabled") {
		if err := validateOperatorListen(operatorAddress); err != nil {
			logger.Error("startup failed",
				"component", "operator.http",
				"operation", "listen_validation",
				"error_code", fault.OperatorSecureTransportRequired,
				"address", operatorAddress,
				"error", err,
			)
			os.Exit(1)
		}
	}

	app := httpapi.New(state, logger, version)
	handler := operatorui.New(app.Handler(), state, operatorAuth, logger)
	publicServer := newHTTPServer(env("AEGISPXE_LISTEN", "127.0.0.1:8090"), handler)
	servers := []*http.Server{publicServer}
	listeners := []string{"public"}
	if !strings.EqualFold(operatorAddress, "disabled") {
		servers = append(servers, newHTTPServer(operatorAddress, handler))
		listeners = append(listeners, "operator")
	}

	errCh := make(chan error, len(servers))
	for index, server := range servers {
		server := server
		listener := listeners[index]
		logger.Info("AegisPXE server starting",
			"component", "server",
			"operation", "listen",
			"version", version,
			"listener", listener,
			"address", server.Addr,
			"write_timeout_ms", serverWriteTimeout.Milliseconds(),
		)
		go func() {
			err := server.ListenAndServe()
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
			if err != nil {
				err = fmt.Errorf("%s listener: %w", listener, err)
			}
			errCh <- err
		}()
	}

	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-errCh:
		if serveErr != nil {
			logger.Error("server stopped unexpectedly", "component", "server", "operation", "serve", "error", serveErr)
			stop()
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for index, server := range servers {
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown failed", "component", "server", "operation", "shutdown", "listener", listeners[index], "error", err)
		}
	}
	if serveErr != nil {
		os.Exit(1)
	}
	logger.Info("AegisPXE server stopped", "component", "server", "operation", "shutdown")
}

func newHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       60 * time.Second,
	}
}

func validateOperatorListen(address string) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil || port == "" {
		return errors.New("operator listener must be a host:port address")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("operator listener must use a loopback IP address")
	}
	return nil
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
