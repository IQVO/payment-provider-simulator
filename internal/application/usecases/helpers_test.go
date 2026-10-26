package usecases_test

import (
	"context"
	"testing"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/memory"
	"github.com/IQVO/payment-provider-simulator/internal/application/usecases"
	"github.com/IQVO/payment-provider-simulator/internal/application/views"
)

var t0 = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

type rig struct {
	deps  usecases.Deps
	clock *memory.Clock
}

func newRig() *rig {
	s := memory.NewStore()
	c := memory.NewClock(t0)
	return &rig{clock: c, deps: usecases.Deps{
		UoW: memory.NewUnitOfWork(s), Authorizations: memory.NewAuthorizations(s),
		Idempotency: memory.NewIdempotency(s), Clock: c, IDs: &memory.IDs{},
	}}
}

func (r *rig) authorize(t *testing.T, key, token string, amount int64) views.Answer {
	t.Helper()
	a, err := usecases.Authorize{Deps: r.deps}.Execute(context.Background(), usecases.AuthorizeInput{
		Key: key, AmountMinor: amount, Currency: "EUR", PaymentMethodToken: token, Reference: "cs-1",
	})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	return a
}
