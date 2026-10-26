package providerauth_test

import (
	"errors"
	"testing"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/domain/providerauth"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providertoken"
	"github.com/IQVO/payment-provider-simulator/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

func hold(t *testing.T, amount int64) *providerauth.Authorization {
	t.Helper()
	a, err := providerauth.Authorize("hold_1", providertoken.Approve, "cs-1", "EUR", amount, t0)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAuthorizeSetsTheHold(t *testing.T) {
	a := hold(t, 1000)
	if a.ID() != "hold_1" || a.Reference() != "cs-1" || a.Currency() != "EUR" || a.Scenario() != providertoken.Approve {
		t.Fatalf("unexpected identity: %+v", a.Snapshot())
	}
	if a.HeldMinor() != 1000 || a.RemainingMinor() != 1000 || a.CapturedMinor() != 0 || a.RefundedMinor() != 0 {
		t.Fatalf("unexpected amounts: %+v", a.Snapshot())
	}
	if a.Status() != providerauth.StatusAuthorized || !a.CreatedAt().Equal(t0) {
		t.Fatalf("unexpected state: %+v", a.Snapshot())
	}
	if want := t0.Add(7 * 24 * time.Hour); !a.ExpiresAt().Equal(want) {
		t.Fatalf("expiresAt = %v, want %v", a.ExpiresAt(), want)
	}
}

func TestAuthorizeExpireEarlyExpiresInMinutes(t *testing.T) {
	a, err := providerauth.Authorize("h", providertoken.ExpireEarly, "r", "USD", 1, t0)
	if err != nil {
		t.Fatal(err)
	}
	if want := t0.Add(5 * time.Minute); !a.ExpiresAt().Equal(want) {
		t.Fatalf("expiresAt = %v, want %v", a.ExpiresAt(), want)
	}
}

func TestAuthorizeValidates(t *testing.T) {
	cases := []struct {
		name     string
		id       providerauth.ID
		sc       providertoken.Scenario
		ref, cur string
		amount   int64
		illegal  bool
	}{
		{"decline scenario", "h", providertoken.Decline, "r", "EUR", 1, true},
		{"timeout scenario", "h", providertoken.Timeout, "r", "EUR", 1, true},
		{"no id", "", providertoken.Approve, "r", "EUR", 1, false},
		{"no reference", "h", providertoken.Approve, "", "EUR", 1, false},
		{"lower-case currency", "h", providertoken.Approve, "r", "eur", 1, false},
		{"short currency", "h", providertoken.Approve, "r", "EU", 1, false},
		{"long currency", "h", providertoken.Approve, "r", "EURO", 1, false},
		{"digit currency", "h", providertoken.Approve, "r", "EU1", 1, false},
		{"zero amount", "h", providertoken.Approve, "r", "EUR", 0, false},
		{"negative amount", "h", providertoken.Approve, "r", "EUR", -5, false},
	}
	for _, c := range cases {
		_, err := providerauth.Authorize(c.id, c.sc, c.ref, c.cur, c.amount, t0)
		want := shared.ErrValidation
		if c.illegal {
			want = shared.ErrIllegalTransition
		}
		if !errors.Is(err, want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, want)
		}
	}
}

func TestCaptureAccumulatesAndClosesTheHold(t *testing.T) {
	a := hold(t, 1000)
	if err := a.Capture("cap_1", 400, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if a.CapturedMinor() != 400 || a.RemainingMinor() != 600 || a.Status() != providerauth.StatusAuthorized {
		t.Fatalf("after partial capture: %+v", a.Snapshot())
	}
	if err := a.Capture("cap_2", 600, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if a.RemainingMinor() != 0 || a.Status() != providerauth.StatusCaptured {
		t.Fatalf("after full capture: %+v", a.Snapshot())
	}
	m := a.Movements()
	if len(m) != 2 || m[0].ID != "cap_1" || m[0].Kind != providerauth.KindCapture || m[1].AmountMinor != 600 {
		t.Fatalf("movements = %+v", m)
	}
}

func TestCaptureRefusals(t *testing.T) {
	a := hold(t, 1000)
	if err := a.Capture("c", 1001, t0); !errors.Is(err, providerauth.ErrOverCapture) {
		t.Fatalf("over capture err = %v", err)
	}
	if err := a.Capture("c", 0, t0); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("zero capture err = %v", err)
	}
	if err := a.Capture("c", -1, t0); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("negative capture err = %v", err)
	}
	if err := a.Capture("c", 1, a.ExpiresAt()); !errors.Is(err, providerauth.ErrHoldExpired) {
		t.Fatalf("capture at expiry err = %v", err)
	}
	if err := a.Capture("c", 1, a.ExpiresAt().Add(-time.Nanosecond)); err != nil {
		t.Fatalf("capture just before expiry err = %v", err)
	}
	if len(a.Movements()) != 1 {
		t.Fatalf("a refused capture must leave no movement: %+v", a.Movements())
	}
}

func TestRefundBoundedByCaptured(t *testing.T) {
	a := hold(t, 1000)
	if err := a.Refund("r", 1, t0); !errors.Is(err, providerauth.ErrOverRefund) {
		t.Fatalf("refund before capture err = %v", err)
	}
	_ = a.Capture("c", 500, t0)
	if err := a.Refund("r1", 300, t0); err != nil {
		t.Fatal(err)
	}
	if err := a.Refund("r2", 201, t0); !errors.Is(err, providerauth.ErrOverRefund) {
		t.Fatalf("over refund err = %v", err)
	}
	if err := a.Refund("r2", 200, t0); err != nil {
		t.Fatal(err)
	}
	if a.RefundedMinor() != 500 {
		t.Fatalf("refunded = %d", a.RefundedMinor())
	}
	if err := a.Refund("r3", 0, t0); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("zero refund err = %v", err)
	}
}

func TestCurrencyLetterBoundaries(t *testing.T) {
	for _, ok := range []string{"AAA", "ZZZ", "AZA"} {
		if _, err := providerauth.Authorize("h", providertoken.Approve, "r", ok, 1, t0); err != nil {
			t.Errorf("currency %q must be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"@AA", "AA[", "A`A", "{AA"} {
		if _, err := providerauth.Authorize("h", providertoken.Approve, "r", bad, 1, t0); !errors.Is(err, shared.ErrValidation) {
			t.Errorf("currency %q must be invalid, got %v", bad, err)
		}
	}
}
