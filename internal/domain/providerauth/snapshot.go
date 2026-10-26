package providerauth

import (
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/domain/providertoken"
)

// Snapshot is the persisted state of an Authorization; adapters store it and
// Restore rebuilds the aggregate without re-running any command.
type Snapshot struct {
	ID            ID
	Scenario      providertoken.Scenario
	Reference     string
	Currency      string
	HeldMinor     int64
	CapturedMinor int64
	RefundedMinor int64
	Status        Status
	ExpiresAt     time.Time
	CreatedAt     time.Time
	SupersededBy  ID
	Movements     []Movement
}

// Snapshot returns the persisted state.
func (a *Authorization) Snapshot() Snapshot {
	return Snapshot{
		ID: a.id, Scenario: a.scenario, Reference: a.reference, Currency: a.currency,
		HeldMinor: a.heldMinor, CapturedMinor: a.capturedMinor, RefundedMinor: a.refundedMinor,
		Status: a.status, ExpiresAt: a.expiresAt, CreatedAt: a.createdAt, SupersededBy: a.supersededBy,
		Movements: a.Movements(),
	}
}

// Restore rebuilds an Authorization from a Snapshot.
func Restore(s Snapshot) *Authorization {
	return &Authorization{
		id: s.ID, scenario: s.Scenario, reference: s.Reference, currency: s.Currency,
		heldMinor: s.HeldMinor, capturedMinor: s.CapturedMinor, refundedMinor: s.RefundedMinor,
		status: s.Status, expiresAt: s.ExpiresAt, createdAt: s.CreatedAt, supersededBy: s.SupersededBy,
		movements: append([]Movement(nil), s.Movements...),
	}
}
