package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/application/idempotency"
	"github.com/IQVO/payment-provider-simulator/internal/application/ports"
)

// The idempotency-key retention (ADR 0005, addendum 2026-11-22): a stored
// answer lives for idempotency.RetentionTTL, then the purge deletes its row.
// The worker runs one pass at start and then every idempotencyPurgeInterval,
// in statements of at most idempotencyPurgeBatch rows so a pass never holds a
// long lock, however far behind it is (order-sourcing #84's design; the port
// shape is last-mile-delivery #86's PurgeExpired). The two values are fixed
// on purpose (like the sibling services): the age is
// idempotency.RetentionTTL, the rest is housekeeping, not configuration —
// expiry is enforced at read, so the interval only bounds how long a dead row
// stays on disk.
const (
	idempotencyPurgeInterval = time.Hour
	idempotencyPurgeBatch    = 500
)

// purgeDrainTimeout bounds how long shutdown waits for the purge loop.
const purgeDrainTimeout = 10 * time.Second

// purgeIdempotencyRecords runs batches until one comes back short (nothing
// expired is left), a pass fails or ctx ends, and returns how many rows it
// deleted. A failed pass is retried by the next tick: the purge is
// idempotent, and a row that outlives its TTL is never replayed, so a
// failure costs only disk.
func purgeIdempotencyRecords(ctx context.Context, store ports.IdempotencyStore, batch int, logger *slog.Logger) int {
	total := 0
	for ctx.Err() == nil {
		n, err := store.PurgeExpired(ctx, batch)
		if err != nil {
			if ctx.Err() == nil {
				logger.ErrorContext(ctx, "idempotency purge failed; the next tick retries", "error", err, "purged", total)
			}
			return total
		}
		total += n
		if n < batch {
			return total
		}
	}
	return total
}

// startIdempotencyPurge runs the retention purge: one pass at once, then one
// every interval, until ctx is cancelled; the returned channel closes when
// the loop has returned. Like the readiness probe the purge only ever logs —
// a failed pass is not a death.
func startIdempotencyPurge(ctx context.Context, store ports.IdempotencyStore, logger *slog.Logger) chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		purgeLoop(ctx, store, logger, idempotencyPurgeInterval, idempotencyPurgeBatch)
	}()
	return done
}

// purgeLoop purges at once and then every interval until ctx ends.
func purgeLoop(ctx context.Context, store ports.IdempotencyStore, logger *slog.Logger, interval time.Duration, batch int) {
	logger.Info("idempotency purge running", "interval", interval.String(), "ttl", idempotency.RetentionTTL.String(), "batch", batch)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if n := purgeIdempotencyRecords(ctx, store, batch, logger); n > 0 {
			logger.InfoContext(ctx, "idempotency purge done", "purged", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// awaitPurge waits (bounded) for the purge loop to return after its context
// is cancelled; a cancelled batch aborts mid-statement, so the wait is short.
func awaitPurge(logger *slog.Logger, done <-chan struct{}) {
	select {
	case <-done:
	case <-time.After(purgeDrainTimeout):
		logger.Warn("the idempotency purge did not stop before the shutdown drain deadline")
	}
}
