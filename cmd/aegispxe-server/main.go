package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
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

	app := httpapi.New(state, logger, version)
	handler := operatorui.New(app.Handler(), state, operatorAuth, logger)
	server := &http.Server{
		Addr:              env("AEGISPXE_LISTEN", "127.0.0.1:8090"),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown failed", "component", "server", "operation", "shutdown", "error", err)
		}
	}()

	logger.Info("AegisPXE server starting", "component", "server", "operation", "listen", "version", version, "address", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server stopped unexpectedly", "component", "server", "operation", "serve", "error", err)
		os.Exit(1)
	}
	logger.Info("AegisPXE server stopped", "component", "server", "operation", "shutdown")
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
