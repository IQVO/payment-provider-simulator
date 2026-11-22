package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/application/ports"
)

// fakePurger answers PurgeExpired from a script and records the limit it got.
type fakePurger struct {
	ports.IdempotencyStore
	script []int
	err    error
	limits []int
	calls  chan struct{}
}

func (f *fakePurger) PurgeExpired(_ context.Context, limit int) (int, error) {
	f.limits = append(f.limits, limit)
	if f.calls != nil {
		f.calls <- struct{}{}
	}
	if f.err != nil {
		return 0, f.err
	}
	if len(f.script) == 0 {
		return 0, nil
	}
	n := f.script[0]
	f.script = f.script[1:]
	return n, nil
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestPurgeRunsBatchesUntilOneComesBackShort(t *testing.T) {
	f := &fakePurger{script: []int{3, 3, 1}}
	n := purgeIdempotencyRecords(t.Context(), f, 3, quietLogger())
	if n != 7 || len(f.limits) != 3 || f.limits[0] != 3 {
		t.Fatalf("purge = %d limits %v, want 7 rows in 3 batches of 3", n, f.limits)
	}
}

func TestPurgeStopsOnAnErrorAndReportsIt(t *testing.T) {
	f := &fakePurger{err: errors.New("boom")}
	n := purgeIdempotencyRecords(t.Context(), f, 3, quietLogger())
	if n != 0 || len(f.limits) != 1 {
		t.Fatalf("purge = %d after %d calls, want one call then stop", n, len(f.limits))
	}
}

func TestPurgeStopsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	f := &fakePurger{script: []int{3}}
	n := purgeIdempotencyRecords(ctx, f, 3, quietLogger())
	if n != 0 || len(f.limits) != 0 {
		t.Fatalf("purge on a cancelled ctx = %d calls %d, want none", n, len(f.limits))
	}
}

// The loop purges at once, again every tick, survives a failing pass and
// returns when the context ends (the interval and batch are parameters; run
// passes the fixed hourly/500 constants).
func TestThePurgeLoopRunsAtOnceThenEveryTickAndStopsOnCancel(t *testing.T) {
	f := &fakePurger{err: errors.New("down"), calls: make(chan struct{}, 16)}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		purgeLoop(ctx, f, quietLogger(), 5*time.Millisecond, 10)
	}()
	for i := 0; i < 3; i++ {
		select {
		case <-f.calls:
		case <-time.After(5 * time.Second):
			t.Fatal("the purge loop did not run")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the purge loop did not stop on cancel")
	}
}

func TestAwaitPurgeReturnsWhenTheLoopIsDone(t *testing.T) {
	done := make(chan struct{})
	close(done)
	start := time.Now()
	awaitPurge(quietLogger(), done)
	if time.Since(start) > time.Second {
		t.Fatal("awaitPurge must return at once when the loop is done")
	}
}
