package repocontract

import (
	"context"
	"testing"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/application/idempotency"
	"github.com/IQVO/payment-provider-simulator/internal/application/views"
)

// IdempotencyRetentionFixture is one Adapters set over ONE empty store, plus
// Age: the hook that makes every record stored so far d older — the memory
// double advances its clock, Postgres moves created_at — the only way a test
// reaches an expired row without waiting a day. A negative d ages records
// back down. Storing a record is fixture-independent (a claim and a complete
// in one unit of work, exactly what usecases.Deps.Once does for a 2xx
// answer), so the suite builds it over Adapters itself.
type IdempotencyRetentionFixture struct {
	Adapters
	Age func(t *testing.T, d time.Duration)
}

// IdempotencyRetentionFactory returns a fresh, empty fixture and registers
// its own cleanup with t.
type IdempotencyRetentionFactory func(t *testing.T) IdempotencyRetentionFixture

// RunIdempotencyRetention runs the retention contract of the
// ports.IdempotencyStore — the 24-hour replay window of
// idempotency.RetentionTTL (ADR 0005, addendum 2026-11-22): the memory
// double and the Postgres store both run it. The window's read side is
// observed through Claim: a LIVE record answers (claimed == false), an
// expired one is claimed again (the key is free).
func RunIdempotencyRetention(t *testing.T, factory IdempotencyRetentionFactory) {
	t.Helper()
	cases := []struct {
		name string
		run  func(*testing.T, IdempotencyRetentionFixture)
	}{
		{"an expired record is claimed again, a live one still answers", retentionAgeGuard},
		{"the purge deletes expired keeps live", retentionPurgeKeepsLive},
		{"one batch per pass", retentionPurgeBatches},
		{"oldest first", retentionPurgeOldestFirst},
		{"a claim past the window refreshes the key", retentionClaimRefreshes},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.run(t, factory(t)) })
	}
}

// retentionPut stores answerID under key: one unit of work, claim then
// complete, exactly what usecases.Deps.Once does for a 2xx answer.
func retentionPut(ctx context.Context, t *testing.T, ad Adapters, key, answerID string) {
	t.Helper()
	err := ad.UoW.Do(ctx, func(ctx context.Context) error {
		if _, claimed, err := ad.Idempotency.Claim(ctx, "authorize", key, "fp-"+key); err != nil || !claimed {
			t.Fatalf("put %s: claim = %v, %v (a fresh key must be claimed)", key, claimed, err)
		}
		return ad.Idempotency.Complete(ctx, "authorize", key, views.Answer{Kind: views.KindDone, ID: answerID})
	})
	if err != nil {
		t.Fatal(err)
	}
}

// retentionAnswerOf claims key: claimed == false means the stored answer
// answered (returned with it), claimed == true means no live record exists.
func retentionAnswerOf(ctx context.Context, t *testing.T, ad Adapters, key string) (idempotency.Record, bool) {
	t.Helper()
	var (
		rec     idempotency.Record
		claimed bool
	)
	err := ad.UoW.Do(ctx, func(ctx context.Context) error {
		rec, claimed, _ = ad.Idempotency.Claim(ctx, "authorize", key, "fp-"+key)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return rec, claimed
}

// retentionAgeGuard: a record a minute past the TTL no longer answers (the
// key is free), one stored after it (a minute old) still replays — the read
// guard that keeps an expired row from answering between expiry and the
// sweep.
func retentionAgeGuard(t *testing.T, f IdempotencyRetentionFixture) {
	ctx := context.Background()
	retentionPut(ctx, t, f.Adapters, "gone", "v-gone")
	f.Age(t, idempotency.RetentionTTL+time.Minute)
	retentionPut(ctx, t, f.Adapters, "edge", "v-edge")
	f.Age(t, time.Minute)
	if _, claimed := retentionAnswerOf(ctx, t, f.Adapters, "gone"); !claimed {
		t.Fatal("claim of a key past the window = answered, want claimed again (the key is free)")
	}
	rec, claimed := retentionAnswerOf(ctx, t, f.Adapters, "edge")
	if claimed || rec.Answer.ID != "v-edge" {
		t.Fatalf("claim of a live key = (%+v, %v), want the stored v-edge answer", rec.Answer, claimed)
	}
}

// retentionPurgeKeepsLive: a record older than the TTL goes, one a minute
// short of it and a fresh one stay and answer; a second pass has nothing
// left to delete.
func retentionPurgeKeepsLive(t *testing.T, f IdempotencyRetentionFixture) {
	ctx := context.Background()
	retentionPut(ctx, t, f.Adapters, "old", "v-old")
	f.Age(t, idempotency.RetentionTTL+time.Minute)
	retentionPut(ctx, t, f.Adapters, "edge", "v-edge")
	f.Age(t, idempotency.RetentionTTL-time.Minute)
	retentionPut(ctx, t, f.Adapters, "live", "v-live")
	if n, err := f.Idempotency.PurgeExpired(ctx, 100); err != nil || n != 1 {
		t.Fatalf("purge = %d %v, want 1 (only the expired record)", n, err)
	}
	if _, claimed := retentionAnswerOf(ctx, t, f.Adapters, "old"); !claimed {
		t.Fatal("the expired record must be gone (claimed again)")
	}
	for key, want := range map[string]string{"edge": "v-edge", "live": "v-live"} {
		if rec, claimed := retentionAnswerOf(ctx, t, f.Adapters, key); claimed || rec.Answer.ID != want {
			t.Fatalf("claim %s after the purge = (%+v, %v), want the stored %s", key, rec.Answer, claimed, want)
		}
	}
	if n, err := f.Idempotency.PurgeExpired(ctx, 100); err != nil || n != 0 {
		t.Fatalf("second purge = %d %v, want 0", n, err)
	}
}

// retentionPurgeBatches: one pass deletes at most limit records, the next
// ones finish the backlog.
func retentionPurgeBatches(t *testing.T, f IdempotencyRetentionFixture) {
	ctx := context.Background()
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		retentionPut(ctx, t, f.Adapters, k, "v-"+k)
	}
	f.Age(t, idempotency.RetentionTTL+time.Hour)
	for _, want := range []int{2, 2, 1, 0} {
		if n, err := f.Idempotency.PurgeExpired(ctx, 2); err != nil || n != want {
			t.Fatalf("purge = %d %v, want %d", n, err, want)
		}
	}
}

// retentionPurgeOldestFirst: with two expired records and a limit of one,
// the older goes. Ageing everything back by more than both ages makes the
// survivor live again, which is what tells the two apart (an expired record
// never answers).
func retentionPurgeOldestFirst(t *testing.T, f IdempotencyRetentionFixture) {
	ctx := context.Background()
	retentionPut(ctx, t, f.Adapters, "older", "v-older")
	f.Age(t, time.Hour)
	retentionPut(ctx, t, f.Adapters, "newer", "v-newer")
	f.Age(t, idempotency.RetentionTTL)
	if n, err := f.Idempotency.PurgeExpired(ctx, 1); err != nil || n != 1 {
		t.Fatalf("purge = %d %v, want 1", n, err)
	}
	f.Age(t, -(idempotency.RetentionTTL + time.Hour)) // the survivor would be live again were it still stored
	if _, claimed := retentionAnswerOf(ctx, t, f.Adapters, "older"); !claimed {
		t.Fatal("the older record must be the deleted one (claimed again)")
	}
	if rec, claimed := retentionAnswerOf(ctx, t, f.Adapters, "newer"); claimed || rec.Answer.ID != "v-newer" {
		t.Fatalf("claim of the survivor = (%+v, %v), want the stored v-newer", rec.Answer, claimed)
	}
}

// retentionClaimRefreshes: the answer of a key whose previous answer has
// expired replaces it (the key is free again), so the purge keeps the row
// and the new answer is the one that replays — the double of the Postgres
// ON CONFLICT DO UPDATE ... WHERE created_at <= now() - TTL.
func retentionClaimRefreshes(t *testing.T, f IdempotencyRetentionFixture) {
	ctx := context.Background()
	retentionPut(ctx, t, f.Adapters, "k", "stale")
	f.Age(t, idempotency.RetentionTTL+time.Hour)
	retentionPut(ctx, t, f.Adapters, "k", "fresh")
	if n, err := f.Idempotency.PurgeExpired(ctx, 100); err != nil || n != 0 {
		t.Fatalf("purge after a refresh = %d %v, want 0 (the refreshed record is live)", n, err)
	}
	if rec, claimed := retentionAnswerOf(ctx, t, f.Adapters, "k"); claimed || rec.Answer.ID != "fresh" {
		t.Fatalf("claim of the refreshed key = (%+v, %v), want the stored fresh answer", rec.Answer, claimed)
	}
}
