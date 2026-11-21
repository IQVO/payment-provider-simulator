// Package idempotency is the data vocabulary of the idempotency store (kept out
// of ports, which holds interfaces only).
package idempotency

import "github.com/IQVO/payment-provider-simulator/internal/application/views"

// Record is what the idempotency store remembers for one (operation, key).
type Record struct {
	// Fingerprint identifies the request content the key was first used with.
	Fingerprint string
	Answer      views.Answer
}

// ReplayClass is the fleet replay class of a problem type (owner decision
// 2026-11-18): FINAL (a function of the request alone) and OUTCOME (the command
// committed state, then refused) answers are stored and replayed; RETRYABLE
// answers are not stored — the key is released and a retry executes again.
// The empty string is "no class": such a refusal is released like RETRYABLE
// (the safe fallback); a problem type without a class fails a unit test.
type ReplayClass string

// The replay classes.
const (
	ReplayFinal     ReplayClass = "FINAL"
	ReplayOutcome   ReplayClass = "OUTCOME"
	ReplayRetryable ReplayClass = "RETRYABLE"
)

// Refusal is the replay metadata of one typed error: the slug of its catalogue
// entry and the class that entry declares. RefusalOf reports ok == false for an
// error no entry matches.
type Refusal struct {
	Slug  string
	Class ReplayClass
}
