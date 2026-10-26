package repocontract

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/IQVO/payment-provider-simulator/internal/application/ports"
	"github.com/IQVO/payment-provider-simulator/internal/application/views"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providerauth"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providertoken"
)

func fixture(t *testing.T, id string) *providerauth.Authorization {
	t.Helper()
	a, err := providerauth.Authorize(providerauth.ID(id), providertoken.ExpireEarly, "cs-1", "EUR", 1000, T0)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Capture("cap_1", 400, T0); err != nil {
		t.Fatal(err)
	}
	if err := a.Refund("ref_1", 100, T0); err != nil {
		t.Fatal(err)
	}
	return a
}

func authorizationRoundTrip(ctx context.Context, t *testing.T, ad Adapters) {
	want := fixture(t, "hold_1")
	if err := ad.Authorizations.Save(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := ad.Authorizations.Get(ctx, "hold_1")
	if err != nil {
		t.Fatal(err)
	}
	assertSameHold(t, got.Snapshot(), want.Snapshot())
}

func assertSameHold(t *testing.T, g, w providerauth.Snapshot) {
	t.Helper()
	if g.ID != w.ID || g.Scenario != w.Scenario || g.Reference != w.Reference || g.Currency != w.Currency || g.Status != w.Status || g.SupersededBy != w.SupersededBy {
		t.Fatalf("round trip changed the identity:\n got %+v\nwant %+v", g, w)
	}
	if g.HeldMinor != w.HeldMinor || g.CapturedMinor != w.CapturedMinor || g.RefundedMinor != w.RefundedMinor {
		t.Fatalf("round trip changed the amounts:\n got %+v\nwant %+v", g, w)
	}
	if !g.ExpiresAt.Equal(w.ExpiresAt) || !g.CreatedAt.Equal(w.CreatedAt) {
		t.Fatalf("round trip changed the instants:\n got %+v\nwant %+v", g, w)
	}
	assertMovements(t, g.Movements)
}

func assertMovements(t *testing.T, m []providerauth.Movement) {
	t.Helper()
	if len(m) != 2 || m[0].ID != "cap_1" || m[1].Kind != providerauth.KindRefund || !m[1].At.Equal(T0) {
		t.Fatalf("movements = %+v", m)
	}
}

func authorizationMiss(ctx context.Context, t *testing.T, ad Adapters) {
	if _, err := ad.Authorizations.Get(ctx, "missing"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func saveUpdates(ctx context.Context, t *testing.T, ad Adapters) {
	a := fixture(t, "hold_1")
	_ = ad.Authorizations.Save(ctx, a)
	next, err := a.Reauthorize("hold_2", 600, T0)
	if err != nil {
		t.Fatal(err)
	}
	if err := ad.Authorizations.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := ad.Authorizations.Save(ctx, next); err != nil {
		t.Fatal(err)
	}
	old, _ := ad.Authorizations.Get(ctx, "hold_1")
	if old.Status() != providerauth.StatusSuperseded || old.SupersededBy() != "hold_2" {
		t.Fatalf("old hold = %+v", old.Snapshot())
	}
	if n, _ := ad.Authorizations.Get(ctx, "hold_2"); n.HeldMinor() != 600 {
		t.Fatalf("new hold = %+v", n.Snapshot())
	}
}

func claimReplay(ctx context.Context, t *testing.T, ad Adapters) {
	answer := views.Answer{Kind: views.KindHold, ID: "hold_1", ExpiresAt: T0, AmountMinor: 5, Currency: "EUR"}
	err := ad.UoW.Do(ctx, func(ctx context.Context) error {
		_, claimed, err := ad.Idempotency.Claim(ctx, "authorize", "k", "fp")
		if err != nil || !claimed {
			t.Fatalf("first claim = %v, %v", claimed, err)
		}
		return ad.Idempotency.Complete(ctx, "authorize", "k", answer)
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = ad.UoW.Do(ctx, func(ctx context.Context) error {
		rec, claimed, err := ad.Idempotency.Claim(ctx, "authorize", "k", "other")
		if err != nil || claimed || rec.Fingerprint != "fp" || rec.Answer.ID != "hold_1" || !rec.Answer.ExpiresAt.Equal(T0) {
			t.Fatalf("replay = %+v, %v, %v", rec, claimed, err)
		}
		if _, claimed, _ := ad.Idempotency.Claim(ctx, "void", "k", "fp"); !claimed {
			t.Fatal("the same key under another operation is a different pair")
		}
		return nil
	})
}

func rollsBack(ctx context.Context, t *testing.T, ad Adapters) {
	boom := errors.New("boom")
	err := ad.UoW.Do(ctx, func(ctx context.Context) error {
		if err := ad.Authorizations.Save(ctx, fixture(t, "hold_1")); err != nil {
			return err
		}
		if _, _, err := ad.Idempotency.Claim(ctx, "authorize", "k", "fp"); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom unchanged", err)
	}
	if _, err := ad.Authorizations.Get(ctx, "hold_1"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("a rolled-back save must vanish, got %v", err)
	}
	_ = ad.UoW.Do(ctx, func(ctx context.Context) error {
		if _, claimed, _ := ad.Idempotency.Claim(ctx, "authorize", "k", "fp"); !claimed {
			t.Fatal("a rolled-back claim must vanish")
		}
		return nil
	})
}

func reentrant(ctx context.Context, t *testing.T, ad Adapters) {
	err := ad.UoW.Do(ctx, func(ctx context.Context) error {
		return ad.UoW.Do(ctx, func(ctx context.Context) error { return ad.Authorizations.Save(ctx, fixture(t, "hold_1")) })
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ad.Authorizations.Get(ctx, "hold_1"); err != nil {
		t.Fatal(err)
	}
}

func concurrentClaims(ctx context.Context, t *testing.T, ad Adapters) {
	var effects atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := ad.UoW.Do(ctx, func(ctx context.Context) error {
				_, claimed, err := ad.Idempotency.Claim(ctx, "captures", "same", "fp")
				if err != nil || !claimed {
					return err
				}
				effects.Add(1)
				return ad.Idempotency.Complete(ctx, "captures", "same", views.Answer{Kind: views.KindDone, ID: "x"})
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if effects.Load() != 1 {
		t.Fatalf("effect ran %d times, want 1", effects.Load())
	}
}
