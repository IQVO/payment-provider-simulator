package memory_test

import (
	"testing"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/memory"
	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/repocontract"
)

func TestRepositoryContract(t *testing.T) {
	repocontract.Run(t, func(*testing.T) repocontract.Adapters {
		s := memory.NewStore()
		return repocontract.Adapters{UoW: memory.NewUnitOfWork(s), Authorizations: memory.NewAuthorizations(s), Idempotency: memory.NewIdempotency(s)}
	})
}

// TestIdempotencyRetentionContract runs the retention contract (ADR 0005,
// addendum 2026-11-22) against the memory double: the settable Clock is the
// store's stamping clock, so Age advances the replay window with it.
func TestIdempotencyRetentionContract(t *testing.T) {
	repocontract.RunIdempotencyRetention(t, func(t *testing.T) repocontract.IdempotencyRetentionFixture {
		clock := memory.NewClock(repocontract.T0)
		s := memory.NewStoreClock(clock)
		return repocontract.IdempotencyRetentionFixture{
			Adapters: repocontract.Adapters{UoW: memory.NewUnitOfWork(s), Idempotency: memory.NewIdempotency(s)},
			Age:      func(t *testing.T, d time.Duration) { clock.Advance(d) },
		}
	})
}
