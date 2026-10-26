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
