package usecases_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/application/ports"
	"github.com/IQVO/payment-provider-simulator/internal/application/usecases"
	"github.com/IQVO/payment-provider-simulator/internal/application/views"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providerauth"
)

func cmd(key, hold string, amount int64) usecases.HoldCommand {
	return usecases.HoldCommand{Key: key, HoldID: hold, AmountMinor: amount}
}

func TestCaptureAndRefundLifecycle(t *testing.T) {
	r := newRig()
	h := r.authorize(t, "a", "tok_approve", 1000)
	ctx := context.Background()
	c, err := usecases.Capture{Deps: r.deps}.Execute(ctx, cmd("c1", h.ID, 600))
	if err != nil || c.Kind != views.KindMovement || c.ID == "" {
		t.Fatalf("capture = %+v, %v", c, err)
	}
	if again, _ := (usecases.Capture{Deps: r.deps}).Execute(ctx, cmd("c1", h.ID, 600)); again != c {
		t.Fatalf("capture replay = %+v", again)
	}
	if _, err := (usecases.Capture{Deps: r.deps}).Execute(ctx, cmd("c2", h.ID, 401)); !errors.Is(err, providerauth.ErrOverCapture) {
		t.Fatalf("over capture err = %v", err)
	}
	if _, err := (usecases.Refund{Deps: r.deps}).Execute(ctx, cmd("r1", h.ID, 200)); err != nil {
		t.Fatal(err)
	}
	if _, err := (usecases.Refund{Deps: r.deps}).Execute(ctx, cmd("r2", h.ID, 401)); !errors.Is(err, providerauth.ErrOverRefund) {
		t.Fatalf("over refund err = %v", err)
	}
	got, err := usecases.GetAuthorization{Deps: r.deps}.Execute(ctx, h.ID)
	if err != nil || got.CapturedMinor != 600 || got.RefundedMinor != 200 || len(got.Movements) != 2 || got.Movements[1].Kind != "REFUND" {
		t.Fatalf("hold = %+v, %v", got, err)
	}
}

func TestFailedCommandRollsBackItsClaim(t *testing.T) {
	r := newRig()
	h := r.authorize(t, "a", "tok_approve", 100)
	ctx := context.Background()
	if _, err := (usecases.Capture{Deps: r.deps}).Execute(ctx, cmd("k", h.ID, 101)); !errors.Is(err, providerauth.ErrOverCapture) {
		t.Fatalf("err = %v", err)
	}
	if _, err := (usecases.Capture{Deps: r.deps}).Execute(ctx, cmd("k", h.ID, 100)); err != nil {
		t.Fatalf("the key of a failed command must stay usable: %v", err)
	}
}

func TestUnknownHoldIsNotFound(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	if _, err := (usecases.Void{Deps: r.deps}).Execute(ctx, cmd("k", "nope", 0)); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("void err = %v", err)
	}
	if _, err := (usecases.GetAuthorization{Deps: r.deps}).Execute(ctx, "nope"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("get err = %v", err)
	}
}

func TestVoidAndReduce(t *testing.T) {
	r := newRig()
	h := r.authorize(t, "a", "tok_approve", 1000)
	ctx := context.Background()
	if d, err := (usecases.Reduce{Deps: r.deps}).Execute(ctx, cmd("r", h.ID, 400)); err != nil || d.Kind != views.KindDone {
		t.Fatalf("reduce = %+v, %v", d, err)
	}
	if _, err := (usecases.Reduce{Deps: r.deps}).Execute(ctx, cmd("r2", h.ID, 401)); !errors.Is(err, providerauth.ErrNotAReduction) {
		t.Fatalf("raise err = %v", err)
	}
	if _, err := (usecases.Void{Deps: r.deps}).Execute(ctx, cmd("v", h.ID, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := (usecases.Void{Deps: r.deps}).Execute(ctx, cmd("v", h.ID, 0)); err != nil {
		t.Fatalf("void replay: %v", err)
	}
	got, _ := usecases.GetAuthorization{Deps: r.deps}.Execute(ctx, h.ID)
	if got.Status != "VOIDED" {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestReauthorizeGivesANewHold(t *testing.T) {
	r := newRig()
	h := r.authorize(t, "a", "tok_expire-early", 1000)
	ctx := context.Background()
	r.clock.Advance(4 * time.Minute)
	n, err := usecases.Reauthorize{Deps: r.deps}.Execute(ctx, cmd("re", h.ID, 800))
	if err != nil || n.Kind != views.KindHold || n.ID == h.ID || n.AmountMinor != 800 || !n.ExpiresAt.Equal(t0.Add(9*time.Minute)) {
		t.Fatalf("reauthorize = %+v, %v", n, err)
	}
	old, _ := usecases.GetAuthorization{Deps: r.deps}.Execute(ctx, h.ID)
	if old.Status != "SUPERSEDED" || old.SupersededBy != n.ID {
		t.Fatalf("old hold = %+v", old)
	}
	if _, err := (usecases.Capture{Deps: r.deps}).Execute(ctx, cmd("c", n.ID, 800)); err != nil {
		t.Fatalf("capture on the new hold: %v", err)
	}
}

func TestCaptureAfterExpiryIsRefused(t *testing.T) {
	r := newRig()
	h := r.authorize(t, "a", "tok_expire-early", 100)
	r.clock.Advance(5 * time.Minute)
	if _, err := (usecases.Capture{Deps: r.deps}).Execute(context.Background(), cmd("c", h.ID, 1)); !errors.Is(err, providerauth.ErrHoldExpired) {
		t.Fatalf("err = %v", err)
	}
}
