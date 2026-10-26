package providerauth_test

import (
	"errors"
	"testing"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/domain/providerauth"
	"github.com/IQVO/payment-provider-simulator/internal/domain/shared"
)

func TestVoidReleasesTheRemainder(t *testing.T) {
	a := hold(t, 1000)
	_ = a.Capture("c", 300, t0)
	if err := a.Void(); err != nil {
		t.Fatal(err)
	}
	if a.Status() != providerauth.StatusVoided || a.HeldMinor() != 300 || a.RemainingMinor() != 0 {
		t.Fatalf("after void: %+v", a.Snapshot())
	}
	if err := a.Void(); !errors.Is(err, shared.ErrIllegalTransition) {
		t.Fatalf("second void err = %v", err)
	}
	if err := a.Capture("c2", 1, t0); !errors.Is(err, shared.ErrIllegalTransition) {
		t.Fatalf("capture after void err = %v", err)
	}
}

func TestVoidAfterFullCaptureIsIllegal(t *testing.T) {
	a := hold(t, 100)
	_ = a.Capture("c", 100, t0)
	if err := a.Void(); !errors.Is(err, shared.ErrIllegalTransition) {
		t.Fatalf("void of captured hold err = %v", err)
	}
}

func TestReduceLowersTheRemainingHold(t *testing.T) {
	a := hold(t, 1000)
	_ = a.Capture("c", 200, t0)
	if err := a.Reduce(500); err != nil {
		t.Fatal(err)
	}
	if a.HeldMinor() != 700 || a.RemainingMinor() != 500 || a.Status() != providerauth.StatusAuthorized {
		t.Fatalf("after reduce: %+v", a.Snapshot())
	}
	if err := a.Reduce(500); err != nil {
		t.Fatalf("reducing to the same remainder must be a no-op, got %v", err)
	}
	if err := a.Reduce(501); !errors.Is(err, providerauth.ErrNotAReduction) {
		t.Fatalf("raise err = %v", err)
	}
	if err := a.Reduce(-1); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("negative err = %v", err)
	}
}

func TestReduceToZeroClosesTheHold(t *testing.T) {
	untouched := hold(t, 1000)
	if err := untouched.Reduce(0); err != nil || untouched.Status() != providerauth.StatusVoided {
		t.Fatalf("untouched reduce to zero: %v %+v", err, untouched.Snapshot())
	}
	captured := hold(t, 1000)
	_ = captured.Capture("c", 400, t0)
	if err := captured.Reduce(0); err != nil || captured.Status() != providerauth.StatusCaptured || captured.HeldMinor() != 400 {
		t.Fatalf("captured reduce to zero: %v %+v", err, captured.Snapshot())
	}
	if err := captured.Reduce(0); !errors.Is(err, shared.ErrIllegalTransition) {
		t.Fatalf("reduce of a closed hold err = %v", err)
	}
}

func TestReauthorizeSupersedesTheHold(t *testing.T) {
	a := hold(t, 1000)
	_ = a.Capture("c", 400, t0)
	later := t0.Add(6 * 24 * time.Hour)
	next, err := a.Reauthorize("hold_2", 600, later)
	if err != nil {
		t.Fatal(err)
	}
	if a.Status() != providerauth.StatusSuperseded || a.SupersededBy() != "hold_2" || a.RemainingMinor() != 0 {
		t.Fatalf("old hold: %+v", a.Snapshot())
	}
	if next.ID() != "hold_2" || next.HeldMinor() != 600 || next.CapturedMinor() != 0 || next.Reference() != "cs-1" || next.Currency() != "EUR" {
		t.Fatalf("new hold: %+v", next.Snapshot())
	}
	if want := later.Add(7 * 24 * time.Hour); !next.ExpiresAt().Equal(want) {
		t.Fatalf("new expiry = %v, want %v", next.ExpiresAt(), want)
	}
	if _, err := a.Reauthorize("hold_3", 1, later); !errors.Is(err, shared.ErrIllegalTransition) {
		t.Fatalf("reauthorize of a superseded hold err = %v", err)
	}
}

func TestReauthorizeValidatesBeforeChangingAnything(t *testing.T) {
	a := hold(t, 1000)
	if _, err := a.Reauthorize("hold_2", 0, t0); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("zero amount err = %v", err)
	}
	if a.Status() != providerauth.StatusAuthorized || a.SupersededBy() != "" {
		t.Fatalf("a refused reauthorize must not change the hold: %+v", a.Snapshot())
	}
}

func TestReauthorizeKeepsTheEarlyExpiryScenario(t *testing.T) {
	a, _ := providerauth.Authorize("h1", "expire-early", "r", "EUR", 10, t0)
	next, err := a.Reauthorize("h2", 10, t0.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if want := t0.Add(6 * time.Minute); !next.ExpiresAt().Equal(want) {
		t.Fatalf("new expiry = %v, want %v", next.ExpiresAt(), want)
	}
}

func TestSnapshotRestoreRoundTrip(t *testing.T) {
	a := hold(t, 1000)
	_ = a.Capture("c", 400, t0)
	_ = a.Refund("r", 100, t0)
	s := a.Snapshot()
	b := providerauth.Restore(s)
	got := b.Snapshot()
	if got.ID != s.ID || got.HeldMinor != s.HeldMinor || got.CapturedMinor != 400 || got.RefundedMinor != 100 ||
		got.Status != s.Status || !got.ExpiresAt.Equal(s.ExpiresAt) || len(got.Movements) != 2 {
		t.Fatalf("round trip changed the snapshot: %+v vs %+v", got, s)
	}
	s.Movements[0].AmountMinor = 1
	if b.Movements()[0].AmountMinor != 400 {
		t.Fatal("Restore must copy the movements")
	}
}
