package http

import (
	"errors"
	"net/http"

	"github.com/IQVO/payment-provider-simulator/internal/application/ports"
	"github.com/IQVO/payment-provider-simulator/internal/application/usecases"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providerauth"
	"github.com/IQVO/payment-provider-simulator/internal/domain/shared"
)

// problemBase is the namespace of this service's RFC 9457 `type` URIs.
const problemBase = "/problems/"

// errMalformedRequest marks a body or parameter the adapter itself rejects.
var errMalformedRequest = errors.New("request is not valid for this operation")

// problem is the fixed (status, slug, title) of one error category.
type problem struct {
	status int
	slug   string
	title  string
}

var internalProblem = problem{http.StatusInternalServerError, "internal-error", "Internal server error"}

func unprocessable(slug, title string) problem {
	return problem{http.StatusUnprocessableEntity, slug, title}
}

func conflict(slug, title string) problem { return problem{http.StatusConflict, slug, title} }

// Problems the adapter writes itself.
var (
	notFound        = problem{http.StatusNotFound, "not-found", "Not found"}
	paymentDeclined = problem{http.StatusPaymentRequired, "payment-declined", "The payment provider declined the authorization"}
)

// problemCatalogue maps every typed error to its problem, in match order. The
// slugs are the ones of apis/openapi.yaml and .claude/rules/rest-api.md.
var problemCatalogue = []struct {
	err error
	p   problem
}{
	{errMalformedRequest, problem{http.StatusBadRequest, "malformed-request", "Malformed request"}},
	{usecases.ErrIdempotencyKeyRequired, problem{http.StatusBadRequest, "idempotency-key-required", "Idempotency-Key header is required"}},
	{usecases.ErrIdempotencyKeyReused, unprocessable("idempotency-key-reused", "Idempotency-Key was already used with a different request")},
	{ports.ErrNotFound, notFound},
	{usecases.ErrProviderTimeout, problem{http.StatusGatewayTimeout, "provider-timeout", "The payment provider timed out; retry with the same Idempotency-Key"}},
	{providerauth.ErrOverCapture, conflict("over-capture", "A capture never exceeds the remaining hold")},
	{providerauth.ErrOverRefund, conflict("over-refund", "Refunds never exceed the captured total")},
	{providerauth.ErrHoldExpired, conflict("hold-expired", "No capture after the hold expired")},
	{providerauth.ErrNotAReduction, unprocessable("not-a-reduction", "Reduce only lowers the remaining hold")},
	{shared.ErrIllegalTransition, conflict("illegal-transition", "The hold does not allow this change in its current state")},
	{shared.ErrValidation, unprocessable("validation-failed", "Validation failed")},
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
