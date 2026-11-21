// Package usecases orchestrates the simulator: each use case is one idempotent
// command (or query) over the ProviderAuthorization aggregate, run in a unit
// of work together with its Idempotency-Key claim.
package usecases

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/application/idempotency"
	"github.com/IQVO/payment-provider-simulator/internal/application/ports"
	"github.com/IQVO/payment-provider-simulator/internal/application/views"
)

// DefaultTimeoutDelay is how long a timeout-scenario authorization stalls
// before answering 504; kept short so consumer tests stay fast (<= 2 s).
const DefaultTimeoutDelay = time.Second

var (
	// ErrIdempotencyKeyRequired: a POST without an Idempotency-Key.
	ErrIdempotencyKeyRequired = errors.New("Idempotency-Key is required") //nolint:staticcheck // header name is the message
	// ErrIdempotencyKeyReused: the key was first used with a different request.
	ErrIdempotencyKeyReused = errors.New("Idempotency-Key was already used with a different request")
	// ErrProviderTimeout: the timeout scenario; the caller treats it as transient.
	ErrProviderTimeout = errors.New("payment provider timed out")
)

// Deps are the collaborators every use case shares.
type Deps struct {
	UoW            ports.UnitOfWork
	Authorizations ports.AuthorizationRepository
	Idempotency    ports.IdempotencyStore
	Clock          ports.Clock
	IDs            ports.IDGenerator
	// Refusals classifies an effect error for the replay-class rule; nil means
	// every refusal is released like RETRYABLE (nothing stored). cmd wires the
	// problem catalogue's implementation.
	Refusals ports.Refusals
	// TimeoutDelay is how long the timeout scenario stalls; zero means none.
	TimeoutDelay time.Duration
}

// keyScope is the single namespace every Idempotency-Key is claimed in: the
// key is global (fleet REST guideline 2.3), so the same key on another
// operation is a different request (ErrIdempotencyKeyReused), never a fresh key.
const keyScope = "*"

// Once runs effect at most once per key: a replay of the same request (same
// operation and inputs) returns the stored answer marked Replayed, the same key
// with another request is ErrIdempotencyKeyReused. The claim, the effect and
// the stored answer commit together; a failed effect leaves no claim behind —
// UNLESS the failure's problem type is FINAL or OUTCOME (replay-class rule,
// owner decision 2026-11-18): then the refusal is stored as the key's answer
// and the unit commits, so a retry replays it byte for byte. RETRYABLE and
// unclassified refusals roll the whole unit back and release the key.
func (d Deps) Once(ctx context.Context, operation, key, fingerprint string,
	effect func(ctx context.Context) (views.Answer, error),
) (views.Answer, error) {
	if key == "" {
		return views.Answer{}, ErrIdempotencyKeyRequired
	}
	var (
		out     views.Answer
		refused error
	)
	fingerprint = operation + "|" + fingerprint
	err := d.UoW.Do(ctx, func(ctx context.Context) error {
		rec, claimed, err := d.Idempotency.Claim(ctx, keyScope, key, fingerprint)
		if err != nil {
			return err
		}
		if !claimed {
			if rec.Fingerprint != fingerprint {
				return ErrIdempotencyKeyReused
			}
			out = rec.Answer
			out.Replayed = true
			return nil
		}
		if out, err = effect(ctx); err != nil {
			if !d.storesRefusal(err) {
				return err
			}
			refusal := views.Answer{Kind: views.KindProblem, Slug: d.refusalSlug(err), Detail: err.Error()}
			if cerr := d.Idempotency.Complete(ctx, keyScope, key, refusal); cerr != nil {
				return cerr
			}
			refused = err
			return nil // commit the stored refusal; the caller still answers err
		}
		return d.Idempotency.Complete(ctx, keyScope, key, out)
	})
	if refused != nil && err == nil {
		err = refused
		out = views.Answer{}
	}
	return out, err
}

// storesRefusal reports whether err's problem type is FINAL or OUTCOME: the
// keyed answer must be remembered. Everything else (RETRYABLE, unclassified, a
// 5xx) is released — the rollback this store always did.
func (d Deps) storesRefusal(err error) bool {
	if d.Refusals == nil {
		return false
	}
	refusal, ok := d.Refusals.RefusalOf(err)
	return ok && (refusal.Class == idempotency.ReplayFinal || refusal.Class == idempotency.ReplayOutcome)
}

// refusalSlug is the catalogue slug of a refusal storesRefusal accepted.
func (d Deps) refusalSlug(err error) string {
	refusal, _ := d.Refusals.RefusalOf(err)
	return refusal.Slug
}

func fingerprint(parts ...any) string { return fmt.Sprintf("%#v", parts) }

// stall waits the timeout-scenario delay or until ctx ends.
func (d Deps) stall(ctx context.Context) error {
	if d.TimeoutDelay <= 0 {
		return nil
	}
	t := time.NewTimer(d.TimeoutDelay)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
