// Package ports declares the application's OUT ports: interfaces only
// (enforced by internal/architecture), plus the sentinels their adapters
// return. Inside a UnitOfWork every repository joins the transaction carried
// by the ctx the unit handed to its function.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/application/idempotency"
	"github.com/IQVO/payment-provider-simulator/internal/application/views"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providerauth"
)

// ErrNotFound is what a repository returns for a Get that matches nothing
// (problem type not-found, 404).
var ErrNotFound = errors.New("not found")

// AuthorizationRepository persists ProviderAuthorization aggregates.
type AuthorizationRepository interface {
	// Get returns the authorization, or ErrNotFound. Inside a unit of work the
	// row is locked until the unit ends, so concurrent commands on one hold
	// serialize.
	Get(ctx context.Context, id providerauth.ID) (*providerauth.Authorization, error)
	// Save inserts or updates the authorization.
	Save(ctx context.Context, a *providerauth.Authorization) error
}

// IdempotencyStore makes every POST idempotent on its Idempotency-Key.
type IdempotencyStore interface {
	// Claim reserves (operation, key) inside the current unit of work. When the
	// pair was claimed before (and that unit committed) it returns the stored
	// record and claimed == false. A concurrent claim of the same pair waits
	// for the other unit to end.
	Claim(ctx context.Context, operation, key, fingerprint string) (rec idempotency.Record, claimed bool, err error)
	// Complete stores the answer of a claimed pair.
	Complete(ctx context.Context, operation, key string, answer views.Answer) error
}

// UnitOfWork runs fn as ONE atomic unit: every repository and store call fn
// makes with the ctx it is handed commits together when fn returns nil, or
// rolls back together when fn returns an error. Do is re-entrant; the error fn
// returns is returned unchanged.
type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// Clock is the service's notion of "now".
type Clock interface {
	Now() time.Time
}

// IDGenerator mints the identifiers the simulator hands out.
type IDGenerator interface {
	// NewHoldID mints a hold id.
	NewHoldID() providerauth.ID
	// NewMovementID mints the id of a capture or refund.
	NewMovementID(kind providerauth.MovementKind) string
	// NewTokenSuffix mints the unique tail of an issued payment-method token.
	NewTokenSuffix() string
}
