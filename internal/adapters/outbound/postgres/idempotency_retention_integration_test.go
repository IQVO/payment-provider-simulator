//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/postgres"
	"github.com/IQVO/payment-provider-simulator/internal/application/idempotency"
	"github.com/IQVO/payment-provider-simulator/internal/testing/pgtest"
)

// The purge's two liveness proofs (order-sourcing #84's two race tests,
// this store's shape): it must never delete a row a request holds — a
// claim refreshing an expired row inside an open transaction — and never
// wait on one: FOR UPDATE SKIP LOCKED skips it, and the age test is
// repeated on the DELETE itself so a row a request just refreshed survives.

// claimSQL is the claim's upsert (postgres.IdempotencyStore.Claim), run by
// hand inside a held transaction: the conflict arm refreshes the expired
// row and the row lock lives until the commit.
const claimSQL = `
	INSERT INTO idempotency_records (operation, key, fingerprint) VALUES ($1, $2, $3)
	ON CONFLICT (operation, key) DO UPDATE SET
		fingerprint = EXCLUDED.fingerprint, answer = NULL, created_at = now()
	WHERE idempotency_records.created_at <= now() - make_interval(secs => $4)`

func TestPurgeNeverDeletesARecordARequestHoldsAndNeverWaitsForIt(t *testing.T) {
	pool := pgtest.NewPool(t)
	ctx := context.Background()
	ttl := idempotency.RetentionTTL.Seconds() // the constant the claim SQL passes

	seed := func(key string, age time.Duration) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			`INSERT INTO idempotency_records (operation, key, fingerprint, created_at)
			 VALUES ('authorize', $1, 'fp-old', now() - $2::interval)`, key, age.String()); err != nil {
			t.Fatal(err)
		}
	}
	seed("held", 30*time.Hour)
	seed("dead", 48*time.Hour)

	// The request: an open transaction whose claim has just refreshed the
	// expired row (the row lock lives until the commit).
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, claimSQL, "authorize", "held", "fp-new", ttl); err != nil {
		t.Fatal(err)
	}

	store := postgres.NewIdempotencyStore(pool)
	purgeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	start := time.Now()
	n, err := store.PurgeExpired(purgeCtx, 100)
	if err != nil || n != 1 {
		t.Fatalf("purge while a request holds a row = %d %v, want 1 (only the unheld dead row) and no wait", n, err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("purge waited %s on the held row: SKIP LOCKED must skip, never wait", elapsed)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	// The committed refresh is live: no later purge may delete it.
	if n, err := store.PurgeExpired(ctx, 100); err != nil || n != 0 {
		t.Fatalf("purge after the request committed = %d %v, want 0: the refreshed row is live", n, err)
	}
	var fingerprint string
	if err := pool.QueryRow(ctx,
		`SELECT fingerprint FROM idempotency_records WHERE operation = 'authorize' AND key = 'held'`).Scan(&fingerprint); err != nil {
		t.Fatal(err)
	}
	if fingerprint != "fp-new" {
		t.Fatalf("fingerprint = %q, want the refreshing request's fp-new", fingerprint)
	}
}
