package http

import (
	"errors"
	"net/http"

	"github.com/IQVO/payment-provider-simulator/internal/application/idempotency"
	"github.com/IQVO/payment-provider-simulator/internal/application/ports"
	"github.com/IQVO/payment-provider-simulator/internal/application/usecases"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providerauth"
	"github.com/IQVO/payment-provider-simulator/internal/domain/shared"
)

// problemBase is the namespace of this service's RFC 9457 `type` URIs.
const problemBase = "/problems/"

// errMalformedRequest marks a body or parameter the adapter itself rejects.
var errMalformedRequest = errors.New("request is not valid for this operation")

// problem is the fixed (status, slug, title) of one error category, with the
// replay class its keyed-route answer carries (fleet rule, owner decision
// 2026-11-18): FINAL and OUTCOME answers are stored and replayed, RETRYABLE
// answers release the key. A problem type without a class fails a unit test.
type problem struct {
	status int
	slug   string
	title  string
	class  idempotency.ReplayClass
}

var internalProblem = problem{http.StatusInternalServerError, "internal-error", "Internal server error", idempotency.ReplayRetryable}

// Problems the adapter writes itself.
var (
	notFound        = problem{http.StatusNotFound, "not-found", "Not found", idempotency.ReplayFinal}
	paymentDeclined = problem{http.StatusPaymentRequired, "payment-declined", "The payment provider declined the authorization", idempotency.ReplayFinal}
)

// problemCatalogue maps every typed error to its problem, in match order. The
// slugs are the ones of apis/openapi.yaml and .claude/rules/rest-api.md.
var problemCatalogue = []struct {
	err error
	p   problem
}{
	{errMalformedRequest, problem{http.StatusBadRequest, "malformed-request", "Malformed request", idempotency.ReplayFinal}},
	{usecases.ErrIdempotencyKeyRequired, problem{http.StatusBadRequest, "idempotency-key-required", "Idempotency-Key header is required", idempotency.ReplayFinal}},
	{usecases.ErrIdempotencyKeyReused, problem{http.StatusConflict, "idempotency-key-reused", "Idempotency-Key was already used with a different request", idempotency.ReplayFinal}},
	{ports.ErrNotFound, notFound},
	{usecases.ErrProviderTimeout, problem{http.StatusGatewayTimeout, "provider-timeout", "The payment provider timed out; retry with the same Idempotency-Key", idempotency.ReplayRetryable}},
	{providerauth.ErrOverCapture, problem{http.StatusConflict, "over-capture", "A capture never exceeds the remaining hold", idempotency.ReplayFinal}},
	{providerauth.ErrOverRefund, problem{http.StatusConflict, "over-refund", "Refunds never exceed the captured total", idempotency.ReplayRetryable}},
	{providerauth.ErrHoldExpired, problem{http.StatusConflict, "hold-expired", "No capture after the hold expired", idempotency.ReplayFinal}},
	{providerauth.ErrNotAReduction, problem{http.StatusUnprocessableEntity, "not-a-reduction", "Reduce only lowers the remaining hold", idempotency.ReplayFinal}},
	{shared.ErrIllegalTransition, problem{http.StatusConflict, "illegal-transition", "The hold does not allow this change in its current state", idempotency.ReplayFinal}},
	{shared.ErrValidation, problem{http.StatusUnprocessableEntity, "validation-failed", "Validation failed", idempotency.ReplayFinal}},
}

// problemFor returns the problem of the first catalogue entry err matches, or
// the internal-error problem.
func problemFor(err error) problem {
	for _, entry := range problemCatalogue {
		if errors.Is(err, entry.err) {
			return entry.p
		}
	}
	return internalProblem
}

// problemBySlug finds the problem a stored refusal answer names: the replay
// path renders a stored FINAL/OUTCOME refusal from the slug it was stored with.
func problemBySlug(slug string) (problem, bool) {
	for _, entry := range problemCatalogue {
		if entry.p.slug == slug {
			return entry.p, true
		}
	}
	for _, p := range []problem{keyRequired, keyInvalid, notFoundResource, paymentDeclined, dependencyUnavailable, internalProblem} {
		if p.slug == slug {
			return p, true
		}
	}
	return problem{}, false
}

// refusals is the ports.Refusals of the problem catalogue: the class-driven
// store (usecases.Deps.Once) asks it whether an effect error's answer is final
// for the request, and under which slug to remember it.
type refusals struct{}

var _ ports.Refusals = refusals{}

// NewRefusals returns the catalogue-backed ports.Refusals. An error no entry
// matches is not ok: the caller releases it like RETRYABLE (safe fallback).
func NewRefusals() ports.Refusals { return refusals{} }

func (refusals) RefusalOf(err error) (idempotency.Refusal, bool) {
	for _, entry := range problemCatalogue {
		if errors.Is(err, entry.err) {
			return idempotency.Refusal{Slug: entry.p.slug, Class: entry.p.class}, true
		}
	}
	return idempotency.Refusal{}, false
}
