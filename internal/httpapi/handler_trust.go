package httpapi

import "net/http"

// HandlerWithBootTrust composes the public machine-trust and signed reporter
// surfaces around the core handler. The outer PXE listener still allowlists
// the exact paths that may cross the firmware/installer trust boundary.
func (s *Server) HandlerWithBootTrust() http.Handler {
	mux := http.NewServeMux()
	s.registerBootTrust(mux)
	s.registerReporterTelemetry(mux)
	mux.Handle("/", s.Handler())
	return mux
}
