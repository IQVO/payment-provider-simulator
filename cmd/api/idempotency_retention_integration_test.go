//go:build integration

package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/postgres"
	"github.com/IQVO/payment-provider-simulator/internal/testing/pgtest"
)

// The retention half of the replay-class rule (ADR 0005, judgement call 4):
// idempotency_records had NO TTL and no purge, so a stored answer replayed
// forever. These proofs drive the REAL binary and the REAL Postgres store
// through the router: past the 24-hour replay window the key is free again —
// the same request re-executes instead of replaying, a different request
// under the expired key is a NEW request instead of 409
// idempotency-key-reused, and the refreshed answer replays while it stays
// live (customer-payments ADR 0008: within the window the replay contract is
// unchanged).

// retentionDB is the test's SQL handle on the booted service's own database:
// it ages key rows (the only way a test reaches an expired row without
// waiting a day), counts them and counts holds by reference.
type retentionDB struct {
	age   func(t *testing.T, key string, d time.Duration)
	count func(t *testing.T, key string) int
	holds func(t *testing.T, reference string) int
}

// bootRetention boots the real binary on a fresh Postgres and returns its
// base URL plus a SQL handle on the SAME database (pgtest.URL hands every
// call its own database, so the handle shares the one the service booted on).
func bootRetention(t *testing.T) (string, retentionDB) {
	t.Helper()
	addr := freeAddr(t)
	url := pgtest.URL(t)
	t.Setenv("DATABASE_URL", url)
	t.Setenv("HTTP_ADDR", addr)
	t.Setenv("SHUTDOWN_DRAIN_DELAY", "0")
	t.Setenv("PROVIDER_TIMEOUT_DELAY", "50ms")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("run returned %v", err)
			}
		case <-time.After(15 * time.Second):
			t.Fatal("run did not stop")
		}
	})
	base := "http://" + addr
	waitReady(t, base)
	pool, err := postgres.NewPool(context.Background(), url)
	if err != nil {
		t.Fatalf("sql pool: %v", err)
	}
	t.Cleanup(pool.Close)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	query := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	return base, retentionDB{
		age: func(t *testing.T, key string, d time.Duration) {
			t.Helper()
			exec(`UPDATE idempotency_records SET created_at = now() - $1::interval WHERE key = $2`, d.String(), key)
		},
		count: func(t *testing.T, key string) int {
			t.Helper()
			return query(`SELECT count(*) FROM idempotency_records WHERE key = $1`, key)
		},
		holds: func(t *testing.T, reference string) int {
			t.Helper()
			return query(`SELECT count(*) FROM provider_authorizations WHERE reference = $1`, reference)
		},
	}
}

func TestExpiredIdempotencyKeysExecuteAgain(t *testing.T) {
	base, db := bootRetention(t)

	t.Run("the same request past the window re-executes instead of replaying", func(t *testing.T) {
		const key = "ret-same"
		body := `{"amountMinor":1500,"currency":"EUR","paymentMethodToken":"tok_approve","reference":"cs-ret"}`
		status, replayed, first := rawPost(t, base+"/authorizations", key, body)
		if status != http.StatusCreated || replayed != "" || !strings.Contains(first, `"id"`) {
			t.Fatalf("first = %d replayed=%q body=%s", status, replayed, first)
		}
		if status, replayed, again := rawPost(t, base+"/authorizations", key, body); status != http.StatusCreated || replayed != "true" || again != first {
			t.Fatalf("replay inside the window = %d replayed=%q, want the stored answer", status, replayed)
		}
		db.age(t, key, 25*time.Hour)
		status, replayed, fresh := rawPost(t, base+"/authorizations", key, body)
		if status != http.StatusCreated || replayed != "" || fresh == first {
			t.Fatalf("past the window = %d replayed=%q body=%s, want a FRESH 201 (a new hold), not the stored %s", status, replayed, fresh, first)
		}
		if n := db.count(t, key); n != 1 {
			t.Fatalf("rows of the refreshed key = %d, want 1 (refreshed in place)", n)
		}
	})

	t.Run("a different request under the expired key is a new request", func(t *testing.T) {
		const key = "ret-other"
		body := `{"amountMinor":1100,"currency":"EUR","paymentMethodToken":"tok_approve","reference":"cs-ret"}`
		if status, replayed, _ := rawPost(t, base+"/authorizations", key, body); status != http.StatusCreated || replayed != "" {
			t.Fatalf("first = %d replayed=%q", status, replayed)
		}
		db.age(t, key, 24*time.Hour+time.Hour)
		other := `{"amountMinor":2200,"currency":"EUR","paymentMethodToken":"tok_approve","reference":"cs-ret"}`
		status, replayed, raw := rawPost(t, base+"/authorizations", key, other)
		if status != http.StatusCreated || replayed != "" || strings.Contains(raw, "idempotency-key-reused") {
			t.Fatalf("another body under the expired key = %d replayed=%q body=%s, want a fresh 201, not 409 idempotency-key-reused", status, replayed, raw)
		}
	})

	t.Run("the refreshed answer replays while it stays live", func(t *testing.T) {
		const key = "ret-refresh"
		body := `{"amountMinor":3300,"currency":"EUR","paymentMethodToken":"tok_approve","reference":"cs-ret"}`
		if status, _, _ := rawPost(t, base+"/authorizations", key, body); status != http.StatusCreated {
			t.Fatalf("first = %d", status)
		}
		db.age(t, key, 30*time.Hour)
		if status, replayed, _ := rawPost(t, base+"/authorizations", key, body); status != http.StatusCreated || replayed != "" {
			t.Fatalf("refresh = %d replayed=%q", status, replayed)
		}
		if status, replayed, again := rawPost(t, base+"/authorizations", key, body); status != http.StatusCreated || replayed != "true" {
			t.Fatalf("replay of the refreshed answer = %d replayed=%q body=%s, want the stored answer with Idempotency-Replayed true", status, replayed, again)
		}
	})

	t.Run("a FINAL refusal expires with its key too", func(t *testing.T) {
		const key = "ret-final"
		body := `{"amountMinor":0,"currency":"EUR","paymentMethodToken":"tok_approve","reference":"cs-ret"}`
		status, replayed, first := rawPost(t, base+"/authorizations", key, body)
		if status != http.StatusUnprocessableEntity || replayed != "" || !strings.Contains(first, "/problems/validation-failed") {
			t.Fatalf("first = %d replayed=%q body=%s", status, replayed, first)
		}
		if status, replayed, again := rawPost(t, base+"/authorizations", key, body); status != http.StatusUnprocessableEntity || replayed != "true" || again != first {
			t.Fatalf("replay inside the window = %d replayed=%q, want the stored refusal", status, replayed)
		}
		db.age(t, key, 25*time.Hour)
		status, replayed, again := rawPost(t, base+"/authorizations", key, body)
		if status != http.StatusUnprocessableEntity || replayed != "" || again != first {
			t.Fatalf("past the window = %d replayed=%q body=%s, want the refusal re-executed WITHOUT the replay header", status, replayed, again)
		}
	})
}
