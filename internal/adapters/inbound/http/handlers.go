package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/IQVO/payment-provider-simulator/internal/application/usecases"
	"github.com/IQVO/payment-provider-simulator/internal/application/views"
)

const idempotencyKeyHeader = "Idempotency-Key"

type tokenRequest struct {
	Scenario string `json:"scenario"`
}

type authorizeRequest struct {
	AmountMinor        int64  `json:"amountMinor"`
	Currency           string `json:"currency"`
	PaymentMethodToken string `json:"paymentMethodToken"`
	Reference          string `json:"reference"`
}

type amountRequest struct {
	AmountMinor int64 `json:"amountMinor"`
}

type tokenResponse struct {
	Token    string `json:"token"`
	Scenario string `json:"scenario"`
}

type holdResponse struct {
	ID          string    `json:"id"`
	ExpiresAt   time.Time `json:"expiresAt"`
	AmountMinor int64     `json:"amountMinor"`
	Currency    string    `json:"currency"`
}

type movementResponse struct {
	ID          string `json:"id"`
	AmountMinor int64  `json:"amountMinor"`
	Currency    string `json:"currency"`
}

type idResponse struct {
	ID string `json:"id"`
}

// writeAnswer renders a use case outcome: a decline is the final 402, anything
// else its resource with 201 (created) or 200 (done).
func writeAnswer(w http.ResponseWriter, r *http.Request, a views.Answer, err error) {
	if err != nil {
		writeError(w, r, err)
		return
	}
	switch a.Kind {
	case views.KindDeclined:
		body := problemBody(r, paymentDeclined, "the payment method was declined: "+a.Code)
		body.Code = a.Code
		writeBody(w, paymentDeclined.status, body)
	case views.KindToken:
		writeJSON(w, http.StatusCreated, tokenResponse{Token: a.Token, Scenario: a.Scenario})
	case views.KindHold:
		writeJSON(w, http.StatusCreated, holdResponse{ID: a.ID, ExpiresAt: a.ExpiresAt, AmountMinor: a.AmountMinor, Currency: a.Currency})
	case views.KindMovement:
		writeJSON(w, http.StatusCreated, movementResponse{ID: a.ID, AmountMinor: a.AmountMinor, Currency: a.Currency})
	default:
		writeJSON(w, http.StatusOK, idResponse{ID: a.ID})
	}
}

func (s *Server) handleIssueToken(w http.ResponseWriter, r *http.Request) {
	var req tokenRequest
	if err := decodeOptional(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	a, err := s.IssueToken.Execute(r.Context(), usecases.IssueTokenInput{Key: r.Header.Get(idempotencyKeyHeader), Scenario: req.Scenario})
	writeAnswer(w, r, a, err)
}

func (s *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	var req authorizeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	a, err := s.Authorize.Execute(r.Context(), usecases.AuthorizeInput{
		Key: r.Header.Get(idempotencyKeyHeader), AmountMinor: req.AmountMinor, Currency: req.Currency,
		PaymentMethodToken: req.PaymentMethodToken, Reference: req.Reference,
	})
	writeAnswer(w, r, a, err)
}

func (s *Server) handleGetAuthorization(w http.ResponseWriter, r *http.Request) {
	h, err := s.GetAuthorization.Execute(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, holdBody(h))
}

// holdOp runs an operation on the hold of the path; withAmount selects whether
// the body carries amountMinor.
func holdOp(withAmount bool, run func(s *Server, r *http.Request, c usecases.HoldCommand) (views.Answer, error)) func(*Server) http.HandlerFunc {
	return func(s *Server) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var req amountRequest
			var err error
			if withAmount {
				err = decodeJSON(r, &req)
			} else {
				err = decodeOptional(r, &struct{}{})
			}
			if err != nil {
				writeError(w, r, err)
				return
			}
			a, err := run(s, r, usecases.HoldCommand{Key: r.Header.Get(idempotencyKeyHeader), HoldID: chi.URLParam(r, "id"), AmountMinor: req.AmountMinor})
			writeAnswer(w, r, a, err)
		}
	}
}

func (s *Server) handleVoid(w http.ResponseWriter, r *http.Request) {
	holdOp(false, func(s *Server, r *http.Request, c usecases.HoldCommand) (views.Answer, error) {
		return s.Void.Execute(r.Context(), c)
	})(s)(w, r)
}

func (s *Server) handleReauthorize(w http.ResponseWriter, r *http.Request) {
	holdOp(true, func(s *Server, r *http.Request, c usecases.HoldCommand) (views.Answer, error) {
		return s.Reauthorize.Execute(r.Context(), c)
	})(s)(w, r)
}

func (s *Server) handleReduce(w http.ResponseWriter, r *http.Request) {
	holdOp(true, func(s *Server, r *http.Request, c usecases.HoldCommand) (views.Answer, error) {
		return s.Reduce.Execute(r.Context(), c)
	})(s)(w, r)
}

func (s *Server) handleCapture(w http.ResponseWriter, r *http.Request) {
	holdOp(true, func(s *Server, r *http.Request, c usecases.HoldCommand) (views.Answer, error) {
		return s.Capture.Execute(r.Context(), c)
	})(s)(w, r)
}

func (s *Server) handleRefund(w http.ResponseWriter, r *http.Request) {
	holdOp(true, func(s *Server, r *http.Request, c usecases.HoldCommand) (views.Answer, error) {
		return s.Refund.Execute(r.Context(), c)
	})(s)(w, r)
}

// decodeOptional is decodeJSON that also accepts an empty body.
func decodeOptional(r *http.Request, dest any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		return errMalformedRequest
	}
	if len(body) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytesReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil || dec.More() {
		return errors.Join(errMalformedRequest, err)
	}
	return nil
}
