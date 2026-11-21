//go:build integration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/postgres"
	"github.com/IQVO/payment-provider-simulator/internal/testing/pgtest"
)

// The fleet replay-class rule (owner decision 2026-11-18; ADR 0005): a retry
// under the same Idempotency-Key is answered from the store only when the first
// answer is final for that request. These proofs drive the REAL binary and the
// REAL Postgres store through the router: a FINAL refusal is stored and
// replayed, a RETRYABLE refusal leaves no key row so its retry executes again
// (and passes once the state changed), and a concurrent duplicate of a stored
// answer replays it.

// rawPost posts body with the key and returns the status, the
// Idempotency-Replayed header and the raw body.
func rawPost(t *testing.T, url, key, body string) (int, string, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Idempotency-Replayed"), string(raw)
}

// bootReplayClasses boots the real binary on a fresh Postgres and returns its
// base URL and a counting pool on the same database.
func bootReplayClasses(t *testing.T) (string, func(key string) int) {
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
		t.Fatalf("counting pool: %v", err)
	}
	t.Cleanup(pool.Close)
	count := func(key string) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(context.Background(),
			`SELECT count(*) FROM idempotency_records WHERE key = $1`, key).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	return base, count
}

// authorizeOk places a hold of amount and returns its id.
func authorizeOk(t *testing.T, base, key, token string, amount int64) string {
	t.Helper()
	body := fmt.Sprintf(`{"amountMinor":%d,"currency":"EUR","paymentMethodToken":%q,"reference":"cs-rc"}`, amount, token)
	status, replayed, raw := rawPost(t, base+"/authorizations", key, body)
	if status != http.StatusCreated || replayed != "" {
		t.Fatalf("authorize = %d replayed=%q body=%s", status, replayed, raw)
	}
	var id struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(raw), &id); err != nil || id.ID == "" {
		t.Fatalf("authorize body %s: %v", raw, err)
	}
	return id.ID
}

func TestReplayClassesOverRealPostgres(t *testing.T) {
	base, rows := bootReplayClasses(t)

	t.Run("a FINAL validation refusal is stored and replayed", func(t *testing.T) {
		const key = "rc-vf"
		body := `{"amountMinor":0,"currency":"EUR","paymentMethodToken":"tok_approve","reference":"cs-vf"}`
		status, replayed, first := rawPost(t, base+"/authorizations", key, body)
		if status != http.StatusUnprocessableEntity || replayed != "" || !strings.Contains(first, "/problems/validation-failed") {
			t.Fatalf("first = %d replayed=%q body=%s", status, replayed, first)
		}
		status, replayed, again := rawPost(t, base+"/authorizations", key, body)
		if status != http.StatusUnprocessableEntity || replayed != "true" || again != first {
			t.Fatalf("replay = %d replayed=%q body=%s want the stored %s answer", status, replayed, again, first)
		}
		other := `{"amountMinor":1000,"currency":"EUR","paymentMethodToken":"tok_approve","reference":"cs-vf"}`
		if status, _, raw := rawPost(t, base+"/authorizations", key, other); status != http.StatusConflict || !strings.Contains(raw, "idempotency-key-reused") {
			t.Fatalf("another body under the stored key = %d %s, want 409 idempotency-key-reused", status, raw)
		}
		if n := rows(key); n != 1 {
			t.Fatalf("key rows after the FINAL refusal = %d, want 1 (stored)", n)
		}
	})

	t.Run("a FINAL not-found refusal is stored and replayed", func(t *testing.T) {
		const key = "rc-nf"
		if n := rows(key); n != 0 {
			t.Fatalf("precondition: key rows = %d", n)
		}
		status, replayed, first := rawPost(t, base+"/authorizations/hold_doesnotexist/refunds", key, `{"amountMinor":100}`)
		if status != http.StatusNotFound || replayed != "" || !strings.Contains(first, "/problems/not-found") {
			t.Fatalf("first = %d replayed=%q body=%s", status, replayed, first)
		}
		status, replayed, again := rawPost(t, base+"/authorizations/hold_doesnotexist/refunds", key, `{"amountMinor":100}`)
		if status != http.StatusNotFound || replayed != "true" || again != first {
			t.Fatalf("replay = %d replayed=%q body=%s want the stored %s answer", status, replayed, again, first)
		}
		if n := rows(key); n != 1 {
			t.Fatalf("key rows after the FINAL refusal = %d, want 1 (stored)", n)
		}
	})

	t.Run("a RETRYABLE over-refund is not stored, retries execute, and it passes once captured", func(t *testing.T) {
		const key = "rc-or"
		_, tokJSON := call(t, http.MethodPost, base+"/tokens", "rc-tok", `{"scenario":"approve"}`)
		token := tokJSON["token"].(string)
		hold := authorizeOk(t, base, "rc-auth", token, 1000)

		status, replayed, first := rawPost(t, base+"/authorizations/"+hold+"/refunds", key, `{"amountMinor":700}`)
		if status != http.StatusConflict || replayed != "" || !strings.Contains(first, "/problems/over-refund") {
			t.Fatalf("over refund = %d replayed=%q body=%s", status, replayed, first)
		}
		if n := rows(key); n != 0 {
			t.Fatalf("key rows after the RETRYABLE refusal = %d, want 0: the key is released", n)
		}
		status, replayed, second := rawPost(t, base+"/authorizations/"+hold+"/refunds", key, `{"amountMinor":700}`)
		if status != http.StatusConflict || replayed != "" {
			t.Fatalf("retry under the released key = %d replayed=%q, want a re-executed 409 without the replay header", status, replayed)
		}
		if second == first {
			t.Fatalf("the retry replayed the first refusal byte for byte: %s", second)
		}
		if status, _, raw := rawPost(t, base+"/authorizations/"+hold+"/captures", "rc-cap", `{"amountMinor":800}`); status != http.StatusCreated {
			t.Fatalf("capture = %d %s", status, raw)
		}
		status, replayed, raw := rawPost(t, base+"/authorizations/"+hold+"/refunds", key, `{"amountMinor":700}`)
		if status != http.StatusCreated || replayed != "" {
			t.Fatalf("the released key's retry after the capture = %d replayed=%q body=%s, want a fresh 201", status, replayed, raw)
		}
		if n := rows(key); n != 1 {
			t.Fatalf("key rows after the success = %d, want 1: a 2xx is stored", n)
		}
		status, replayed, again := rawPost(t, base+"/authorizations/"+hold+"/refunds", key, `{"amountMinor":700}`)
		if status != http.StatusCreated || replayed != "true" || again != raw {
			t.Fatalf("replay of the success = %d replayed=%q body=%s want the stored %s answer", status, replayed, again, raw)
		}
	})

	t.Run("a concurrent duplicate waits and replays the stored answer", func(t *testing.T) {
		const key = "rc-race"
		_, tokJSON := call(t, http.MethodPost, base+"/tokens", "rc-tok2", `{"scenario":"approve"}`)
		token := tokJSON["token"].(string)
		body := fmt.Sprintf(`{"amountMinor":1234,"currency":"EUR","paymentMethodToken":%q,"reference":"cs-race"}`, token)

		start := make(chan struct{})
		results := make([]struct {
			status   int
			replayed string
			body     string
		}, 2)
		var wg sync.WaitGroup
		for i := range results {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				s, r, b := rawPost(t, base+"/authorizations", key, body)
				results[i].status, results[i].replayed, results[i].body = s, r, b
			}(i)
		}
		close(start)
		wg.Wait()

		fresh, replayed := 0, 0
		for _, r := range results {
			if r.status != http.StatusCreated {
				t.Fatalf("duplicate answer = %d body=%s", r.status, r.body)
			}
			switch r.replayed {
			case "":
				fresh++
			case "true":
				replayed++
			default:
				t.Fatalf("Idempotency-Replayed = %q", r.replayed)
			}
		}
		if fresh != 1 || replayed != 1 {
			t.Fatalf("duplicate pair: %d fresh + %d replayed, want 1 + 1 (the loser replays the winner's answer)", fresh, replayed)
		}
		if results[0].body != results[1].body {
			t.Fatalf("the duplicate's answer differs: %s vs %s", results[0].body, results[1].body)
		}
		if n := rows(key); n != 1 {
			t.Fatalf("key rows after the race = %d, want 1", n)
		}
	})
}
