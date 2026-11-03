//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/memory"
	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/postgres"
	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/postgres/pgtx"
	"github.com/IQVO/payment-provider-simulator/internal/application/usecases"
	"github.com/IQVO/payment-provider-simulator/internal/application/views"
	"github.com/IQVO/payment-provider-simulator/internal/testing/pgtest"
)

// Keeps the testcontainers import reachable for the architecture fitness test
// (Postgres integration tests must start their database via testcontainers;
// the database itself comes from pgtest.NewPool).
var _ = tcpostgres.Run

// This simulator has no transaction-joining HTTP middleware: each use case
// claims its Idempotency-Key and runs its effect in ONE UnitOfWork, and every
// failure leaves that function as a Go error that rolls the whole transaction
// back (nothing is cached). These tests pin the invariant the savepoint fix
// guarantees elsewhere: an answer computed over an ABORTED transaction is never
// returned or cached, and the key stays free to retry.

func abortTx(t *testing.T, ctx context.Context) {
	t.Helper()
	tx, ok := pgtx.From(ctx)
	if !ok {
		t.Fatal("the unit of work did not bind a transaction")
	}
	for i := 0; i < 2; i++ {
		if _, err := tx.Exec(ctx, `INSERT INTO idempotency_records (operation, key, fingerprint) VALUES ('dup', 'dup', 'f')`); err != nil {
			return // second insert: 23505 aborts the transaction
		}
	}
	t.Fatal("the duplicate insert did not abort the transaction")
}

func TestIdempotency_AnswerOverAbortedTransactionIsNeverCached(t *testing.T) {
	pool := pgtest.NewPool(t)
	uow, store := postgres.NewUnitOfWork(pool), postgres.NewIdempotencyStore(pool)
	ctx := context.Background()

	err := uow.Do(ctx, func(ctx context.Context) error {
		if _, claimed, err := store.Claim(ctx, "*", "k-lie", "fp"); err != nil || !claimed {
			t.Fatalf("claim = %v, %v", claimed, err)
		}
		abortTx(t, ctx) // the effect swallows a mapped statement error ...
		return store.Complete(ctx, "*", "k-lie", views.Answer{Kind: views.KindDone})
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "25P02" {
		t.Fatalf("recording over an aborted transaction = %v, want SQLSTATE 25P02 (never a cached answer)", err)
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_records WHERE key = 'k-lie'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rows for the failed key = %d, %v; the key must stay free", n, err)
	}
	err = uow.Do(ctx, func(ctx context.Context) error {
		_, claimed, err := store.Claim(ctx, "*", "k-lie", "fp")
		if err == nil && !claimed {
			t.Fatal("the retry did not get a fresh claim")
		}
		return err
	})
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
}

func TestIdempotency_DeclinedAnswerIsCommittedAndReplayedOverPostgres(t *testing.T) {
	pool := pgtest.NewPool(t)
	deps := usecases.Deps{
		UoW: postgres.NewUnitOfWork(pool), Authorizations: postgres.NewAuthorizationRepository(pool),
		Idempotency: postgres.NewIdempotencyStore(pool),
		Clock:       memory.NewClock(time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)), IDs: &memory.IDs{},
	}
	in := usecases.AuthorizeInput{Key: "k-decline", AmountMinor: 100, Currency: "EUR", PaymentMethodToken: "tok_decline_x", Reference: "cs-1"}
	first, err := usecases.Authorize{Deps: deps}.Execute(context.Background(), in)
	if err != nil || first.Kind != views.KindDeclined {
		t.Fatalf("first = %+v, %v; want the decline answer", first, err)
	}
	replay, err := usecases.Authorize{Deps: deps}.Execute(context.Background(), in)
	if err != nil || !replay.Replayed || replay.Kind != views.KindDeclined {
		t.Fatalf("replay = %+v, %v; want the cached decline", replay, err)
	}
}
