// Package repocontract is the behavioural contract every port adapter (the
// memory doubles and Postgres) must satisfy. One suite, run against both, is
// what keeps the doubles honest.
package repocontract

import (
	"context"
	"testing"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/application/ports"
)

// Adapters is the set of ports under test, built fresh for each subtest.
type Adapters struct {
	UoW            ports.UnitOfWork
	Authorizations ports.AuthorizationRepository
	Idempotency    ports.IdempotencyStore
}

// Factory builds Adapters over an empty store.
type Factory func(t *testing.T) Adapters

// T0 is the instant fixtures are stamped with (microsecond precision).
var T0 = time.Date(2026, 10, 26, 9, 0, 0, 123000, time.UTC)

// Run executes the whole contract.
func Run(t *testing.T, newAdapters Factory) {
	t.Helper()
	ctx := context.Background()
	t.Run("authorization round trip", func(t *testing.T) { authorizationRoundTrip(ctx, t, newAdapters(t)) })
	t.Run("authorization miss is ErrNotFound", func(t *testing.T) { authorizationMiss(ctx, t, newAdapters(t)) })
	t.Run("save updates in place", func(t *testing.T) { saveUpdates(ctx, t, newAdapters(t)) })
	t.Run("idempotency claim and replay", func(t *testing.T) { claimReplay(ctx, t, newAdapters(t)) })
	t.Run("unit of work rolls back", func(t *testing.T) { rollsBack(ctx, t, newAdapters(t)) })
	t.Run("unit of work is re-entrant", func(t *testing.T) { reentrant(ctx, t, newAdapters(t)) })
	t.Run("concurrent claims run the effect once", func(t *testing.T) { concurrentClaims(ctx, t, newAdapters(t)) })
}
