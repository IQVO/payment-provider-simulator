package providerauth

import (
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/domain/shared"
)

func (a *Authorization) requireAuthorized(op string) error {
	if a.status != StatusAuthorized {
		return shared.Illegal("cannot %s a hold that is %s", op, a.status)
	}
	return nil
}

// Capture takes amountMinor of the remaining hold; the hold becomes CAPTURED
// once nothing remains.
func (a *Authorization) Capture(movementID string, amountMinor int64, now time.Time) error {
	if err := a.requireAuthorized("capture"); err != nil {
		return err
	}
	if !now.Before(a.expiresAt) {
		return ErrHoldExpired
	}
	if amountMinor <= 0 {
		return shared.Validation("amountMinor must be positive")
	}
	if amountMinor > a.RemainingMinor() {
		return ErrOverCapture
	}
	a.capturedMinor += amountMinor
	a.movements = append(a.movements, Movement{ID: movementID, Kind: KindCapture, AmountMinor: amountMinor, At: now})
	a.settle()
	return nil
}

// Refund returns amountMinor of captured money; it works in any state.
func (a *Authorization) Refund(movementID string, amountMinor int64, now time.Time) error {
	if amountMinor <= 0 {
		return shared.Validation("amountMinor must be positive")
	}
	if a.refundedMinor+amountMinor > a.capturedMinor {
		return ErrOverRefund
	}
	a.refundedMinor += amountMinor
	a.movements = append(a.movements, Movement{ID: movementID, Kind: KindRefund, AmountMinor: amountMinor, At: now})
	return nil
}

// Void releases the whole remaining hold.
func (a *Authorization) Void() error {
	if err := a.requireAuthorized("void"); err != nil {
		return err
	}
	a.heldMinor = a.capturedMinor
	a.status = StatusVoided
	return nil
}

// Reduce lowers the remaining hold to remainingMinor (partial release); it
// never raises it. Reducing to zero closes the hold.
func (a *Authorization) Reduce(remainingMinor int64) error {
	if err := a.requireAuthorized("reduce"); err != nil {
		return err
	}
	if remainingMinor < 0 {
		return shared.Validation("amountMinor must not be negative")
	}
	if remainingMinor > a.RemainingMinor() {
		return ErrNotAReduction
	}
	a.heldMinor = a.capturedMinor + remainingMinor
	a.settle()
	return nil
}

// Reauthorize replaces the hold by a fresh one of amountMinor, valid again for
// the scenario's validity; this hold becomes SUPERSEDED.
func (a *Authorization) Reauthorize(newID ID, amountMinor int64, now time.Time) (*Authorization, error) {
	if err := a.requireAuthorized("reauthorize"); err != nil {
		return nil, err
	}
	next, err := Authorize(newID, a.scenario, a.reference, a.currency, amountMinor, now)
	if err != nil {
		return nil, err
	}
	a.heldMinor = a.capturedMinor
	a.status = StatusSuperseded
	a.supersededBy = newID
	return next, nil
}

// settle closes a hold with nothing left to capture: CAPTURED when something
// was captured, VOIDED when nothing was.
func (a *Authorization) settle() {
	if a.RemainingMinor() > 0 {
		return
	}
	if a.capturedMinor > 0 {
		a.status = StatusCaptured
		return
	}
	a.status = StatusVoided
}
