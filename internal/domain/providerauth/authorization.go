// Package providerauth is the ProviderAuthorization aggregate: one hold the
// simulated payment provider placed on a payment method, with the captures and
// refunds taken against it. The invariants it protects: captures never exceed
// the hold, refunds never exceed what was captured, nothing is captured after
// the hold expired, and a voided, fully captured or superseded hold accepts no
// further hold change.
package providerauth

import (
	"errors"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/domain/providertoken"
	"github.com/IQVO/payment-provider-simulator/internal/domain/shared"
)

// ID identifies a hold ("hold_..."); it is the id the storefront stores.
type ID string

// Status is where a hold is in its life.
type Status string

// The hold states. AUTHORIZED is the only state that accepts hold changes.
const (
	StatusAuthorized Status = "AUTHORIZED"
	StatusCaptured   Status = "CAPTURED"
	StatusVoided     Status = "VOIDED"
	StatusSuperseded Status = "SUPERSEDED"
)

// Domain errors; each maps to a problem type in the HTTP adapter.
var (
	// ErrOverCapture: a capture would exceed the remaining hold.
	ErrOverCapture = errors.New("capture exceeds the remaining hold")
	// ErrOverRefund: a refund would exceed the captured total.
	ErrOverRefund = errors.New("refund exceeds the captured total")
	// ErrHoldExpired: no capture after the hold expired.
	ErrHoldExpired = errors.New("the hold expired")
	// ErrNotAReduction: reduce only lowers the remaining hold.
	ErrNotAReduction = errors.New("reduce cannot raise the remaining hold")
)

// MovementKind tells a capture from a refund.
type MovementKind string

// The movement kinds.
const (
	KindCapture MovementKind = "CAPTURE"
	KindRefund  MovementKind = "REFUND"
)

// Movement is one capture or refund against the hold.
type Movement struct {
	ID          string
	Kind        MovementKind
	AmountMinor int64
	At          time.Time
}

// Authorization is the aggregate root.
type Authorization struct {
	id            ID
	scenario      providertoken.Scenario
	reference     string
	currency      string
	heldMinor     int64
	capturedMinor int64
	refundedMinor int64
	status        Status
	expiresAt     time.Time
	createdAt     time.Time
	supersededBy  ID
	movements     []Movement
}

// Authorize places a new hold. Only approve and expire-early tokens reach it:
// the other scenarios never produce a hold.
func Authorize(id ID, sc providertoken.Scenario, reference, currency string, amountMinor int64, now time.Time) (*Authorization, error) {
	if sc != providertoken.Approve && sc != providertoken.ExpireEarly {
		return nil, shared.Illegal("scenario %q never produces a hold", sc)
	}
	if id == "" {
		return nil, shared.Validation("a hold needs an id")
	}
	if reference == "" {
		return nil, shared.Validation("reference is required")
	}
	if !validCurrency(currency) {
		return nil, shared.Validation("currency %q is not a three-letter upper-case ISO 4217 code", currency)
	}
	if amountMinor <= 0 {
		return nil, shared.Validation("amountMinor must be positive")
	}
	return &Authorization{
		id: id, scenario: sc, reference: reference, currency: currency, heldMinor: amountMinor,
		status: StatusAuthorized, expiresAt: now.Add(sc.HoldValidity()), createdAt: now,
	}, nil
}

func validCurrency(c string) bool {
	if len(c) != 3 {
		return false
	}
	for _, r := range c {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

// ID returns the hold id.
func (a *Authorization) ID() ID { return a.id }

// Scenario returns the scenario of the token the hold was placed with.
func (a *Authorization) Scenario() providertoken.Scenario { return a.scenario }

// Reference returns the caller's reference (the checkout session id).
func (a *Authorization) Reference() string { return a.reference }

// Currency returns the ISO 4217 code.
func (a *Authorization) Currency() string { return a.currency }

// HeldMinor is the total the hold covers (captured part included).
func (a *Authorization) HeldMinor() int64 { return a.heldMinor }

// CapturedMinor is the sum of the captures.
func (a *Authorization) CapturedMinor() int64 { return a.capturedMinor }

// RefundedMinor is the sum of the refunds.
func (a *Authorization) RefundedMinor() int64 { return a.refundedMinor }

// RemainingMinor is what can still be captured.
func (a *Authorization) RemainingMinor() int64 { return a.heldMinor - a.capturedMinor }

// Status returns the hold state.
func (a *Authorization) Status() Status { return a.status }

// ExpiresAt returns the instant the hold stops being capturable.
func (a *Authorization) ExpiresAt() time.Time { return a.expiresAt }

// CreatedAt returns the instant the hold was placed.
func (a *Authorization) CreatedAt() time.Time { return a.createdAt }

// SupersededBy returns the hold that replaced this one by reauthorization.
func (a *Authorization) SupersededBy() ID { return a.supersededBy }

// Movements returns a copy of the captures and refunds, oldest first.
func (a *Authorization) Movements() []Movement { return append([]Movement(nil), a.movements...) }
