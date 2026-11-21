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
	// KindProblem is a STORED refusal answer: a FINAL or OUTCOME problem the
	// keyed-run engine remembered under the key instead of rolling the claim
	// back. Slug names its catalogue entry; Detail is the exact detail of the
	// first answer, so a replay is byte for byte the first refusal.
	KindProblem Kind = "PROBLEM"
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
	// Slug and Detail carry a KindProblem answer (a stored refusal).
	Slug   string `json:"slug,omitempty"`
	Detail string `json:"detail,omitempty"`
	// Replayed is true on the answer of a replay; never stored.
	Replayed bool `json:"-"`
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
