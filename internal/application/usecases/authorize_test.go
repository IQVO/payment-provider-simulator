package usecases_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/application/usecases"
	"github.com/IQVO/payment-provider-simulator/internal/application/views"
	"github.com/IQVO/payment-provider-simulator/internal/domain/shared"
)

func TestIssueTokenNamesItsScenario(t *testing.T) {
	r := newRig()
	a, err := usecases.IssueToken{Deps: r.deps}.Execute(context.Background(), usecases.IssueTokenInput{Key: "k", Scenario: "decline"})
	if err != nil || a.Kind != views.KindToken || a.Scenario != "decline" || a.Token != "tok_decline_t1" {
		t.Fatalf("answer = %+v, err %v", a, err)
	}
	again, _ := usecases.IssueToken{Deps: r.deps}.Execute(context.Background(), usecases.IssueTokenInput{Key: "k", Scenario: "decline"})
	if again != a {
		t.Fatalf("same key must return the same token: %+v vs %+v", again, a)
	}
	if _, err := (usecases.IssueToken{Deps: r.deps}).Execute(context.Background(), usecases.IssueTokenInput{Key: "k", Scenario: "approve"}); !errors.Is(err, usecases.ErrIdempotencyKeyReused) {
		t.Fatalf("reused key err = %v", err)
	}
	if _, err := (usecases.IssueToken{Deps: r.deps}).Execute(context.Background(), usecases.IssueTokenInput{Key: "k2", Scenario: "nope"}); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("bad scenario err = %v", err)
	}
	if _, err := (usecases.IssueToken{Deps: r.deps}).Execute(context.Background(), usecases.IssueTokenInput{}); !errors.Is(err, usecases.ErrIdempotencyKeyRequired) {
		t.Fatalf("missing key err = %v", err)
	}
}

func TestAuthorizeApproveIsIdempotent(t *testing.T) {
	r := newRig()
	a := r.authorize(t, "k1", "tok_approve", 1000)
	if a.Kind != views.KindHold || a.ID != "hold_1" || a.AmountMinor != 1000 || !a.ExpiresAt.Equal(t0.Add(7*24*time.Hour)) {
		t.Fatalf("answer = %+v", a)
	}
	if b := r.authorize(t, "k1", "tok_approve", 1000); b != a {
		t.Fatalf("replay = %+v, want %+v", b, a)
	}
	if c := r.authorize(t, "k2", "tok_approve", 1000); c.ID == a.ID {
		t.Fatalf("a new key must place a new hold")
	}
	_, err := usecases.Authorize{Deps: r.deps}.Execute(context.Background(), usecases.AuthorizeInput{
		Key: "k1", AmountMinor: 999, Currency: "EUR", PaymentMethodToken: "tok_approve", Reference: "cs-1"})
	if !errors.Is(err, usecases.ErrIdempotencyKeyReused) {
		t.Fatalf("same key other amount err = %v", err)
	}
}

func TestAuthorizeDeclineIsFinalAndStored(t *testing.T) {
	r := newRig()
	a := r.authorize(t, "k", "tok_decline_x", 500)
	if a.Kind != views.KindDeclined || a.Code != "CARD_DECLINED" {
		t.Fatalf("answer = %+v", a)
	}
	if b := r.authorize(t, "k", "tok_decline_x", 500); b != a {
		t.Fatalf("replay = %+v", b)
	}
	_, err := usecases.Authorize{Deps: r.deps}.Execute(context.Background(), usecases.AuthorizeInput{
		Key: "k9", AmountMinor: 0, Currency: "EUR", PaymentMethodToken: "tok_decline", Reference: "r"})
	if !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("decline with zero amount err = %v", err)
	}
}

func TestAuthorizeExpireEarlyExpiresInMinutes(t *testing.T) {
	r := newRig()
	a := r.authorize(t, "k", "tok_expire-early", 100)
	if !a.ExpiresAt.Equal(t0.Add(5 * time.Minute)) {
		t.Fatalf("expiresAt = %v", a.ExpiresAt)
	}
}

func TestAuthorizeTimeoutStallsThenFails(t *testing.T) {
	r := newRig()
	r.deps.TimeoutDelay = 30 * time.Millisecond
	start := time.Now()
	_, err := usecases.Authorize{Deps: r.deps}.Execute(context.Background(), usecases.AuthorizeInput{
		Key: "k", AmountMinor: 1, Currency: "EUR", PaymentMethodToken: "tok_timeout", Reference: "r"})
	if !errors.Is(err, usecases.ErrProviderTimeout) || time.Since(start) < 30*time.Millisecond {
		t.Fatalf("err = %v after %v", err, time.Since(start))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = usecases.Authorize{Deps: r.deps}.Execute(ctx, usecases.AuthorizeInput{
		Key: "k", AmountMinor: 1, Currency: "EUR", PaymentMethodToken: "tok_timeout", Reference: "r"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled err = %v", err)
	}
	r.deps.TimeoutDelay = 0
	_, err = usecases.Authorize{Deps: r.deps}.Execute(context.Background(), usecases.AuthorizeInput{
		Key: "k", AmountMinor: 1, Currency: "EUR", PaymentMethodToken: "tok_timeout", Reference: "r"})
	if !errors.Is(err, usecases.ErrProviderTimeout) {
		t.Fatalf("no-delay err = %v", err)
	}
}

func TestAuthorizeRefusesCardNumbersAndBadInput(t *testing.T) {
	r := newRig()
	for _, token := range []string{"4111111111111111", "garbage"} {
		_, err := usecases.Authorize{Deps: r.deps}.Execute(context.Background(), usecases.AuthorizeInput{
			Key: "k", AmountMinor: 1, Currency: "EUR", PaymentMethodToken: token, Reference: "r"})
		if !errors.Is(err, shared.ErrValidation) {
			t.Errorf("token %q err = %v", token, err)
		}
	}
	_, err := usecases.Authorize{Deps: r.deps}.Execute(context.Background(), usecases.AuthorizeInput{
		Key: "k", AmountMinor: 1, Currency: "eur", PaymentMethodToken: "tok_approve", Reference: "r"})
	if !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("bad currency err = %v", err)
	}
	if r.authorize(t, "k", "tok_approve", 5).ID == "" {
		t.Fatal("a failed attempt must not burn the key")
	}
}
