package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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
	"github.com/Ostsee-Developer/AegisPXE/internal/operatorpasskey"
	"github.com/Ostsee-Developer/AegisPXE/internal/operatorui"
	"github.com/Ostsee-Developer/AegisPXE/internal/store"
)

var version = "dev"

const (
	serverWriteTimeout = 10 * time.Minute
	studioLogCapacity  = 2000
)

type namedHTTPServer struct {
	name   string
	server *http.Server
}

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
	logBuffer := observability.NewLogBuffer(studioLogCapacity)
	logger := observability.New(io.MultiWriter(os.Stdout, logBuffer), level)
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
		logger.Error("startup failed", "component", "operator.auth", "operation", "recovery_key", "error_code", fault.StorageFailure, "error", err)
		os.Exit(1)
	}
	passkeys, err := operatorpasskey.New(
		env("AEGISPXE_WEBAUTHN_RP_ID", ""),
		splitConfigList(env("AEGISPXE_WEBAUTHN_ORIGINS", "")),
		logger,
	)
	if err != nil {
		logger.Error("startup failed", "component", "operator.auth", "operation", "webauthn_configuration", "error_code", fault.OperatorAuthenticationFailed, "error", err)
		os.Exit(1)
	}

	proxyTrust, err := operatorui.ParseTrustedProxy(
		env("AEGISPXE_TRUSTED_PROXY_CIDRS", ""),
		env("AEGISPXE_TRUSTED_PROXY_IDENTITY_HEADER", "Remote-User"),
		env("AEGISPXE_TRUSTED_PROXY_PROTO_HEADER", "X-Forwarded-Proto"),
	)
	if err != nil {
		logger.Error("startup failed", "component", "operator.proxy", "operation", "configuration", "error_code", fault.OperatorSecureTransportRequired, "error", err)
		os.Exit(1)
	}

	pxeAddress := envFirst("0.0.0.0:8090", "AEGISPXE_PXE_LISTEN", "AEGISPXE_LISTEN")
	studioAddress := envFirst("127.0.0.1:8091", "AEGISPXE_STUDIO_LISTEN", "AEGISPXE_OPERATOR_LISTEN")
	if !strings.EqualFold(studioAddress, "disabled") {
		if err := validateStudioListen(studioAddress, proxyTrust.Enabled()); err != nil {
			logger.Error("startup failed",
				"component", "operator.http",
				"operation", "listen_validation",
				"error_code", fault.OperatorSecureTransportRequired,
				"address", studioAddress,
				"trusted_proxy_enabled", proxyTrust.Enabled(),
				"error", err,
			)
			os.Exit(1)
		}
	}

	app := httpapi.New(state, logger, version)
	publicHandler := app.HandlerWithBootTrust()
	servers := []namedHTTPServer{{
		name:   "pxe",
		server: newHTTPServer(pxeAddress, pxeSurface(publicHandler)),
	}}
	if !strings.EqualFold(studioAddress, "disabled") {
		studioHandler := operatorui.NewDashboardWithTrustedProxy(publicHandler, state, operatorAuth, passkeys, logBuffer, logger, proxyTrust)
		studioHandler = studioSurface(studioHandler)
		studioHandler = operatorui.RequireTrustedProxyOrLoopback(studioHandler, proxyTrust, logger)
		servers = append(servers, namedHTTPServer{name: "studio", server: newHTTPServer(studioAddress, studioHandler)})
	}

	errCh := make(chan error, len(servers))
	for _, item := range servers {
		item := item
		logger.Info("AegisPXE server starting",
			"component", "server",
			"operation", "listen",
			"version", version,
			"listener", item.name,
			"address", item.server.Addr,
			"trusted_proxy_enabled", item.name == "studio" && proxyTrust.Enabled(),
			"webauthn_enabled", item.name == "studio" && passkeys != nil,
			"write_timeout_ms", serverWriteTimeout.Milliseconds(),
		)
		go func() {
			err := item.server.ListenAndServe()
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
			if err != nil {
				err = fmt.Errorf("%s listener: %w", item.name, err)
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
	for _, item := range servers {
		if err := item.server.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown failed", "component", "server", "operation", "shutdown", "listener", item.name, "error", err)
		}
	}
	if serveErr != nil {
		os.Exit(1)
	}
	logger.Info("AegisPXE server stopped", "component", "server", "operation", "shutdown")
}

func newHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: serverWriteTimeout, IdleTimeout: 60 * time.Second}
}

func pxeSurface(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/healthz" || strings.HasPrefix(path, "/boot/") || path == "/api/v1/discovery" || path == "/api/v1/discovery.ipxe" || installerPXEAPIPath(path) {
			next.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})
}

func installerPXEAPIPath(path string) bool {
	if !strings.HasPrefix(path, "/api/v1/installations/") {
		return false
	}
	for _, suffix := range []string{
		"/telemetry/events",
		"/telemetry/logs",
		"/trust/enroll",
		"/trust/status",
		"/trust/challenge",
		"/trust/prove",
	} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

func studioSurface(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			http.Redirect(w, r, "/ui/", http.StatusTemporaryRedirect)
			return
		}
		if path == "/healthz" || strings.HasPrefix(path, "/ui/") || path == "/api/v1/machines" || strings.HasPrefix(path, "/api/v1/machines/") {
			next.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})
}

func validateStudioListen(address string, trustedProxyEnabled bool) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil || port == "" {
		return errors.New("studio listener must be a host:port address")
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return errors.New("studio listener must use an IP literal")
	}
	if !ip.IsLoopback() && !trustedProxyEnabled {
		return errors.New("non-loopback studio listener requires trusted proxy configuration")
	}
	return nil
}

func splitConfigList(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n'
	})
}

func envFirst(fallback string, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return fallback
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
