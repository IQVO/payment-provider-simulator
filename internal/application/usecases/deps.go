// Package usecases orchestrates the simulator: each use case is one idempotent
// command (or query) over the ProviderAuthorization aggregate, run in a unit
// of work together with its Idempotency-Key claim.
package usecases

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	// TimeoutDelay is how long the timeout scenario stalls; zero means none.
	TimeoutDelay time.Duration
}

// once runs effect at most once per (operation, key): a replay of the same
// request returns the stored answer, the same key with another request is
// ErrIdempotencyKeyReused. The claim, the effect and the stored answer commit
// together; a failed effect leaves no claim behind.
func (d Deps) once(ctx context.Context, operation, key, fingerprint string,
	effect func(ctx context.Context) (views.Answer, error),
) (views.Answer, error) {
	if key == "" {
		return views.Answer{}, ErrIdempotencyKeyRequired
	}
	var out views.Answer
	err := d.UoW.Do(ctx, func(ctx context.Context) error {
		rec, claimed, err := d.Idempotency.Claim(ctx, operation, key, fingerprint)
		if err != nil {
			return err
		}
		if !claimed {
			if rec.Fingerprint != fingerprint {
				return ErrIdempotencyKeyReused
			}
			out = rec.Answer
			return nil
		}
		if out, err = effect(ctx); err != nil {
			return err
		}
		return d.Idempotency.Complete(ctx, operation, key, out)
	})
	return out, err
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
