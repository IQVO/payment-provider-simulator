package usecases

import (
	"context"

	"github.com/IQVO/payment-provider-simulator/internal/application/views"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providerauth"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providertoken"
	"github.com/IQVO/payment-provider-simulator/internal/domain/shared"
)

// Authorize places a hold on the payment method a token stands for. The
// token's scenario decides the outcome: approve and expire-early place a hold,
// decline answers a final decline, timeout stalls and fails transiently.
type Authorize struct{ Deps }

// AuthorizeInput is the request body of POST /authorizations.
type AuthorizeInput struct {
	Key                string
	AmountMinor        int64
	Currency           string
	PaymentMethodToken string
	Reference          string
}

// Execute implements the use case.
func (u Authorize) Execute(ctx context.Context, in AuthorizeInput) (views.Answer, error) {
	sc, err := providertoken.ScenarioOf(in.PaymentMethodToken)
	if err != nil {
		return views.Answer{}, err
	}
	if sc == providertoken.Timeout {
		if err := u.stall(ctx); err != nil {
			return views.Answer{}, err
		}
		return views.Answer{}, ErrProviderTimeout
	}
	fp := fingerprint(in.AmountMinor, in.Currency, in.PaymentMethodToken, in.Reference)
	return u.once(ctx, "authorize", in.Key, fp, func(ctx context.Context) (views.Answer, error) {
		if sc == providertoken.Decline {
			if err := validateAmount(in.AmountMinor); err != nil {
				return views.Answer{}, err
			}
			return views.Answer{Kind: views.KindDeclined, Code: providertoken.DeclineCode}, nil
		}
		a, err := providerauth.Authorize(u.IDs.NewHoldID(), sc, in.Reference, in.Currency, in.AmountMinor, u.Clock.Now())
		if err != nil {
			return views.Answer{}, err
		}
		if err := u.Authorizations.Save(ctx, a); err != nil {
			return views.Answer{}, err
		}
		return holdAnswer(a), nil
	})
}

func validateAmount(amountMinor int64) error {
	if amountMinor <= 0 {
		return shared.Validation("amountMinor must be positive")
	}
	return nil
}

func holdAnswer(a *providerauth.Authorization) views.Answer {
	return views.Answer{
		Kind: views.KindHold, ID: string(a.ID()), ExpiresAt: a.ExpiresAt(),
		AmountMinor: a.HeldMinor(), Currency: a.Currency(),
	}
}
