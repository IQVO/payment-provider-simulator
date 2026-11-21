package usecases_test

import (
	"context"
	"errors"
	"testing"

	"github.com/IQVO/payment-provider-simulator/internal/application/idempotency"
	"github.com/IQVO/payment-provider-simulator/internal/application/usecases"
	"github.com/IQVO/payment-provider-simulator/internal/application/views"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providerauth"
	"github.com/IQVO/payment-provider-simulator/internal/domain/shared"
)

// classRefusals is a ports.Refusals test double: the classes the test declares,
// matched by errors.Is like the production catalogue.
type classRefusals map[error]idempotency.Refusal

func (c classRefusals) RefusalOf(err error) (idempotency.Refusal, bool) {
	for e, r := range c {
		if errors.Is(err, e) {
			return r, true
		}
	}
	return idempotency.Refusal{}, false
}

// classified is a rig whose keyed-run engine classifies like ADR 0005.
func classified(t *testing.T, classes classRefusals) usecases.Deps {
	t.Helper()
	r := newRig()
	r.deps.Refusals = classes
	return r.deps
}

func authorizeInput(key string, amount int64) usecases.AuthorizeInput {
	return usecases.AuthorizeInput{
		Key: key, AmountMinor: amount, Currency: "EUR",
		PaymentMethodToken: "tok_approve_1", Reference: "cs-rc",
	}
}

// A FINAL refusal is stored: the retry replays it (Replayed, the stored slug
// and detail, no error), and the same key with another request is the reuse
// conflict.
func TestOnceStoresAFinalRefusalAndReplaysIt(t *testing.T) {
	deps := classified(t, classRefusals{
		shared.ErrValidation: {Slug: "validation-failed", Class: idempotency.ReplayFinal},
	})
	ctx := context.Background()

	_, err := usecases.Authorize{Deps: deps}.Execute(ctx, authorizeInput("k-final", 0))
	if !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("first = %v, want validation-failed", err)
	}
	firstDetail := err.Error()

	answer, err := usecases.Authorize{Deps: deps}.Execute(ctx, authorizeInput("k-final", 0))
	if err != nil {
		t.Fatalf("replay = %v, want the stored refusal", err)
	}
	if !answer.Replayed || answer.Kind != views.KindProblem || answer.Slug != "validation-failed" {
		t.Fatalf("replay = %+v, want the stored validation-failed problem marked Replayed", answer)
	}
	if answer.Detail != firstDetail {
		t.Fatalf("replay detail %q, want the first answer's %q", answer.Detail, firstDetail)
	}

	if _, err = (usecases.Authorize{Deps: deps}).Execute(ctx, authorizeInput("k-final", 1000)); !errors.Is(err, usecases.ErrIdempotencyKeyReused) {
		t.Fatalf("another request under the stored key = %v, want ErrIdempotencyKeyReused", err)
	}
}

// A RETRYABLE refusal is not stored: the key is released, so the retry
// executes again and a different request under the key is a NEW request.
func TestOnceReleasesARetryableRefusal(t *testing.T) {
	deps := classified(t, classRefusals{
		providerauth.ErrOverCapture: {Slug: "over-capture", Class: idempotency.ReplayRetryable},
	})
	ctx := context.Background()
	hold := usecases.HoldCommand{Key: "h", HoldID: newHold(t, deps), AmountMinor: 400}

	if _, err := (usecases.Capture{Deps: deps}).Execute(ctx, usecases.HoldCommand{Key: "k-retry", HoldID: hold.HoldID, AmountMinor: 401}); !errors.Is(err, providerauth.ErrOverCapture) {
		t.Fatalf("over capture = %v", err)
	}
	// Same key, SAME request: it executes again (still over), never a replay.
	answer, err := (usecases.Capture{Deps: deps}).Execute(ctx, usecases.HoldCommand{Key: "k-retry", HoldID: hold.HoldID, AmountMinor: 401})
	if !errors.Is(err, providerauth.ErrOverCapture) || answer.Replayed {
		t.Fatalf("retry = %v replayed=%v, want a re-executed over-capture without the replay mark", err, answer.Replayed)
	}
	// A different request under the released key is NEW, not a reuse conflict.
	if answer, err = (usecases.Capture{Deps: deps}).Execute(ctx, usecases.HoldCommand{Key: "k-retry", HoldID: hold.HoldID, AmountMinor: 100}); err != nil || answer.Kind != views.KindMovement {
		t.Fatalf("different request under the released key = %v %+v, want a fresh capture", err, answer)
	}
}

// An unclassified refusal (no entry matches) is released like RETRYABLE: the
// fleet's safe fallback. A nil Refusals behaves the same.
func TestOnceReleasesUnclassifiedRefusals(t *testing.T) {
	for _, tc := range []struct {
		name     string
		refusals classRefusals
	}{
		{name: "unclassified", refusals: classRefusals{}}, // nothing matches
		{name: "unwired"}, // Deps.Refusals left nil
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig()
			r.deps.Refusals = tc.refusals
			ctx := context.Background()

			_, err := usecases.Authorize{Deps: r.deps}.Execute(ctx, authorizeInput("k-"+tc.name, 0))
			if !errors.Is(err, shared.ErrValidation) {
				t.Fatalf("refusal = %v, want validation-failed", err)
			}
			answer, err := usecases.Authorize{Deps: r.deps}.Execute(ctx, authorizeInput("k-"+tc.name, 1000))
			if err != nil || answer.Kind != views.KindHold {
				t.Fatalf("different request under the released key = %v %+v, want a fresh hold", err, answer)
			}
		})
	}
}

// newHold places a hold of amount for the capture tests.
func newHold(t *testing.T, deps usecases.Deps) string {
	t.Helper()
	answer, err := usecases.Authorize{Deps: deps}.Execute(context.Background(), authorizeInput("hold-key", 400))
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	return answer.ID
}
