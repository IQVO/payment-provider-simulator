// Package views holds the answers the use cases return. An Answer is also what
// the idempotency store remembers per key, so a replayed request returns the
// first answer byte for byte.
package views

import "time"

// Kind says which operation family an Answer belongs to.
type Kind string

// The answer kinds.
const (
	KindToken    Kind = "TOKEN"
	KindHold     Kind = "HOLD"
	KindMovement Kind = "MOVEMENT"
	KindDone     Kind = "DONE"
	KindDeclined Kind = "DECLINED"
)

// Answer is the outcome of one idempotent POST. Only the fields of its Kind
// are set.
type Answer struct {
	Kind        Kind      `json:"kind"`
	ID          string    `json:"id,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt,omitzero"`
	AmountMinor int64     `json:"amountMinor,omitempty"`
	Currency    string    `json:"currency,omitempty"`
	Token       string    `json:"token,omitempty"`
	Scenario    string    `json:"scenario,omitempty"`
	Code        string    `json:"code,omitempty"`
}

// Hold is the read model of GET /authorizations/{id}.
type Hold struct {
	ID             string
	Status         string
	Scenario       string
	Reference      string
	Currency       string
	HeldMinor      int64
	CapturedMinor  int64
	RefundedMinor  int64
	RemainingMinor int64
	ExpiresAt      time.Time
	CreatedAt      time.Time
	SupersededBy   string
	Movements      []Movement
}

// Movement is one capture or refund of a Hold.
type Movement struct {
	ID          string
	Kind        string
	AmountMinor int64
	At          time.Time
}
