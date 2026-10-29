package usecases

import (
	"context"

	"github.com/IQVO/payment-provider-simulator/internal/application/views"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providerauth"
)

// HoldCommand is the input every operation on an existing hold shares.
type HoldCommand struct {
	Key         string
	HoldID      string
	AmountMinor int64
}

// mutate loads the hold under the unit of work's lock, applies change, saves
// it and answers with answer(hold).
func (d Deps) mutate(ctx context.Context, operation string, c HoldCommand,
	change func(a *providerauth.Authorization) (*providerauth.Authorization, error),
) (views.Answer, error) {
	fp := fingerprint(c.HoldID, c.AmountMinor)
	return d.once(ctx, operation, c.Key, fp, func(ctx context.Context) (views.Answer, error) {
		a, err := d.Authorizations.Get(ctx, providerauth.ID(c.HoldID))
		if err != nil {
			return views.Answer{}, err
		}
		result, err := change(a)
		if err != nil {
			return views.Answer{}, err
		}
		if err := d.Authorizations.Save(ctx, a); err != nil {
			return views.Answer{}, err
		}
		if result != a {
			if err := d.Authorizations.Save(ctx, result); err != nil {
				return views.Answer{}, err
			}
			return holdAnswer(result), nil
		}
		return views.Answer{Kind: views.KindDone, ID: c.HoldID}, nil
	})
}

// Void releases the whole remaining hold.
type Void struct{ Deps }

// Execute implements the use case.
func (u Void) Execute(ctx context.Context, c HoldCommand) (views.Answer, error) {
	return u.mutate(ctx, "void", c, func(a *providerauth.Authorization) (*providerauth.Authorization, error) {
		return a, a.Void()
	})
}

// Reduce lowers the remaining hold (partial release).
type Reduce struct{ Deps }

// Execute implements the use case.
func (u Reduce) Execute(ctx context.Context, c HoldCommand) (views.Answer, error) {
	return u.mutate(ctx, "reduce", c, func(a *providerauth.Authorization) (*providerauth.Authorization, error) {
		return a, a.Reduce(c.AmountMinor)
	})
}

// Reauthorize replaces the hold by a fresh one before it expires.
type Reauthorize struct{ Deps }

// Execute implements the use case.
func (u Reauthorize) Execute(ctx context.Context, c HoldCommand) (views.Answer, error) {
	return u.mutate(ctx, "reauthorize", c, func(a *providerauth.Authorization) (*providerauth.Authorization, error) {
		return a.Reauthorize(u.IDs.NewHoldID(), c.AmountMinor, u.Clock.Now())
	})
}

// movement is the shared shape of a capture and a refund.
func (d Deps) movement(ctx context.Context, operation string, kind providerauth.MovementKind, c HoldCommand,
	apply func(a *providerauth.Authorization, id string) error,
) (views.Answer, error) {
	fp := fingerprint(c.HoldID, c.AmountMinor)
	return d.once(ctx, operation, c.Key, fp, func(ctx context.Context) (views.Answer, error) {
		a, err := d.Authorizations.Get(ctx, providerauth.ID(c.HoldID))
		if err != nil {
			return views.Answer{}, err
		}
		id := d.IDs.NewMovementID(kind)
		if err := apply(a, id); err != nil {
			return views.Answer{}, err
		}
		if err := d.Authorizations.Save(ctx, a); err != nil {
			return views.Answer{}, err
		}
		return views.Answer{Kind: views.KindMovement, ID: id, AmountMinor: c.AmountMinor, Currency: a.Currency()}, nil
	})
}

// Capture takes money from the hold.
type Capture struct{ Deps }

// Execute implements the use case.
func (u Capture) Execute(ctx context.Context, c HoldCommand) (views.Answer, error) {
	return u.movement(ctx, "captures", providerauth.KindCapture, c, func(a *providerauth.Authorization, id string) error {
		return a.Capture(id, c.AmountMinor, u.Clock.Now())
	})
}

// Refund returns captured money.
type Refund struct{ Deps }

// Execute implements the use case.
func (u Refund) Execute(ctx context.Context, c HoldCommand) (views.Answer, error) {
	return u.movement(ctx, "refunds", providerauth.KindRefund, c, func(a *providerauth.Authorization, id string) error {
		return a.Refund(id, c.AmountMinor, u.Clock.Now())
	})
}
