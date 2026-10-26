//go:build integration

package postgres_test

import (
	"context"
	"testing"

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
