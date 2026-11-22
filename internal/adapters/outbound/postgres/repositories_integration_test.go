//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/postgres"
	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/repocontract"
	"github.com/IQVO/payment-provider-simulator/internal/testing/pgtest"
)

func TestRepositoryContract(t *testing.T) {
	repocontract.Run(t, func(t *testing.T) repocontract.Adapters {
		pool := pgtest.NewPool(t)
		return repocontract.Adapters{
			UoW: postgres.NewUnitOfWork(pool), Authorizations: postgres.NewAuthorizationRepository(pool),
			Idempotency: postgres.NewIdempotencyStore(pool),
		}
	})
}

// TestIdempotencyRetentionContract runs the retention contract (ADR 0005,
// addendum 2026-11-22) against the Postgres store: Age moves created_at,
// the double of the memory clock advancing.
func TestIdempotencyRetentionContract(t *testing.T) {
	repocontract.RunIdempotencyRetention(t, func(t *testing.T) repocontract.IdempotencyRetentionFixture {
		pool := pgtest.NewPool(t)
		return repocontract.IdempotencyRetentionFixture{
			Adapters: repocontract.Adapters{
				UoW: postgres.NewUnitOfWork(pool), Idempotency: postgres.NewIdempotencyStore(pool),
			},
			Age: func(t *testing.T, d time.Duration) {
				t.Helper()
				if _, err := pool.Exec(context.Background(),
					`UPDATE idempotency_records SET created_at = created_at - $1::interval`, d.String()); err != nil {
					t.Fatal(err)
				}
			},
		}
	})
}

func tableCount(t *testing.T, url string) int {
	t.Helper()
	pool, err := postgres.NewPool(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name <> 'schema_migrations'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMigrationsGoUpDownUp(t *testing.T) {
	url := pgtest.EmptyURL(t)
	if err := postgres.RunMigrations(url); err != nil {
		t.Fatalf("up: %v", err)
	}
	if n := tableCount(t, url); n != 2 {
		t.Fatalf("after up: %d tables, want 2", n)
	}
	if err := postgres.RunMigrations(url); err != nil {
		t.Fatalf("a second up must be a no-op: %v", err)
	}
	if err := postgres.RollbackMigrations(url); err != nil {
		t.Fatalf("down: %v", err)
	}
	if n := tableCount(t, url); n != 0 {
		t.Fatalf("after down: %d tables remain", n)
	}
	if err := postgres.RunMigrations(url); err != nil {
		t.Fatalf("up again: %v", err)
	}
}
