package usecases

import (
	"context"

	"github.com/IQVO/payment-provider-simulator/internal/application/views"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providerauth"
)

// GetAuthorization reads one hold with its captures and refunds (a test aid:
// the real provider's dashboard).
type GetAuthorization struct{ Deps }

// Execute returns the hold, or ports.ErrNotFound.
func (u GetAuthorization) Execute(ctx context.Context, id string) (views.Hold, error) {
	a, err := u.Authorizations.Get(ctx, providerauth.ID(id))
	if err != nil {
		return views.Hold{}, err
	}
	h := views.Hold{
		ID: string(a.ID()), Status: string(a.Status()), Scenario: string(a.Scenario()), Reference: a.Reference(),
		Currency: a.Currency(), HeldMinor: a.HeldMinor(), CapturedMinor: a.CapturedMinor(), RefundedMinor: a.RefundedMinor(),
		RemainingMinor: a.RemainingMinor(), ExpiresAt: a.ExpiresAt(), CreatedAt: a.CreatedAt(), SupersededBy: string(a.SupersededBy()),
	}
	for _, m := range a.Movements() {
		h.Movements = append(h.Movements, views.Movement{ID: m.ID, Kind: string(m.Kind), AmountMinor: m.AmountMinor, At: m.At})
	}
	return h, nil
}
