package httpapi

import "net/http"

// HandlerWithBootTrust composes the public boot-trust enrollment/proof surface
// around the core handler. Keeping this explicit makes the PXE listener's
// externally reachable installer API auditable in cmd/aegispxe-server.
func (s *Server) HandlerWithBootTrust() http.Handler {
	mux := http.NewServeMux()
	s.registerBootTrust(mux)
	mux.Handle("/", s.Handler())
	return mux
}
