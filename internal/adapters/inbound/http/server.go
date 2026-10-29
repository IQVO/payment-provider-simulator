// Package http is the REST inbound adapter: every route of apis/openapi.yaml on
// a chi router, RFC 9457 problem documents for every error and the liveness and
// readiness endpoints. It has no auth middleware (fleet decision 2026-09-11;
// internal/architecture's TestNoAuthMiddlewareReintroduced).
package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/IQVO/payment-provider-simulator/internal/application/usecases"
)

// Server holds the use cases the handlers call.
type Server struct {
	IssueToken       usecases.IssueToken
	Authorize        usecases.Authorize
	GetAuthorization usecases.GetAuthorization
	Void             usecases.Void
	Reauthorize      usecases.Reauthorize
	Reduce           usecases.Reduce
	Capture          usecases.Capture
	Refund           usecases.Refund

	// Readiness backs GET /readyz; nil means always ready (unit tests).
	Readiness *Readiness
}

// NewRouter builds the chi router for every endpoint of apis/openapi.yaml.
func NewRouter(s *Server) http.Handler {
	r := chi.NewRouter()
	// A NUL (U+0000) in the path, the query or a body is a uniform client error,
	// never a Postgres 22021 -> 500.
	r.Use(rejectNUL)

	r.Get("/healthz", handleHealthz)
	r.Get("/readyz", s.handleReadyz)

	r.Post("/tokens", s.handleIssueToken)
	r.Post("/authorizations", s.handleAuthorize)
	r.Get("/authorizations/{id}", s.handleGetAuthorization)
	r.Post("/authorizations/{id}/void", s.handleVoid)
	r.Post("/authorizations/{id}/reauthorize", s.handleReauthorize)
	r.Post("/authorizations/{id}/reduce", s.handleReduce)
	r.Post("/authorizations/{id}/captures", s.handleCapture)
	r.Post("/authorizations/{id}/refunds", s.handleRefund)
	return r
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, statusBody{Status: "ok"})
}
