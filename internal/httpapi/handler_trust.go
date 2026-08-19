package httpapi

import "net/http"

// HandlerWithBootTrust composes the machine-trust and signed reporter API
// surfaces around the core handler. Reporter boot injection is intentionally
// not registered here: the runtime initramfs experiments were not proven on
// the real UEFI path and must not replace the last known-good Debian boot
// contract. Trust/telemetry APIs stay available for focused integration tests
// while a separately reviewed reporter delivery mechanism is designed.
func (s *Server) HandlerWithBootTrust() http.Handler {
	mux := http.NewServeMux()
	s.registerBootTrust(mux)
	s.registerReporterTelemetry(mux)
	mux.Handle("/", s.Handler())
	return ensureRequestContext(s.logger, mux)
}
