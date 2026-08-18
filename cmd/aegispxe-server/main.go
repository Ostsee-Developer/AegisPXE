package main

import (
	"context"
	"encoding/json"
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
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
	"github.com/Ostsee-Developer/AegisPXE/internal/observability"
	"github.com/Ostsee-Developer/AegisPXE/internal/store"
)

var version = "0.0.2-dev"

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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := state.Ping(r.Context()); err != nil {
			logger.ErrorContext(r.Context(), "health check failed", "component", "server", "operation", "health", "error_code", fault.StorageFailure, "error", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": version})
	})

	server := &http.Server{
		Addr:              env("AEGISPXE_LISTEN", "127.0.0.1:8090"),
		Handler:           requestLog(logger, mux),
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

func requestLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" || len(requestID) > 128 {
			var err error
			requestID, err = idgen.New("req_")
			if err != nil {
				logger.ErrorContext(r.Context(), "request ID allocation failed", "component", "http", "operation", "request_id", "error_code", fault.StorageFailure, "error", err)
				http.Error(w, "internal request tracking failure", http.StatusInternalServerError)
				return
			}
		}
		w.Header().Set("X-Request-ID", requestID)
		started := time.Now()
		next.ServeHTTP(w, r)
		logger.DebugContext(r.Context(), "http request", "component", "http", "operation", "request", "request_id", requestID, "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds())
	})
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
