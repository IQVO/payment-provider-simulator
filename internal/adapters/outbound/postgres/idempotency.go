package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/IQVO/payment-provider-simulator/internal/application/idempotency"
	"github.com/IQVO/payment-provider-simulator/internal/application/ports"
	"github.com/IQVO/payment-provider-simulator/internal/application/views"
)

// IdempotencyStore is the Postgres ports.IdempotencyStore. The retention
// (ADR 0005, addendum 2026-11-22) lives with the data: a row older than
// idempotency.RetentionTTL never answers a replay, a claim past the window
// refreshes the expired row, and PurgeExpired deletes the dead rows the
// read already ignores.
type IdempotencyStore struct{ pool *pgxpool.Pool }

var _ ports.IdempotencyStore = (*IdempotencyStore)(nil)

// NewIdempotencyStore constructs the store over pool.
func NewIdempotencyStore(pool *pgxpool.Pool) *IdempotencyStore { return &IdempotencyStore{pool: pool} }

// ttlSeconds is the retention window as the SQL statements' parameter (the
// same idempotency.RetentionTTL on every predicate).
func ttlSeconds() float64 { return idempotency.RetentionTTL.Seconds() }

// Claim implements ports.IdempotencyStore. The INSERT .. ON CONFLICT waits
// for a concurrent transaction that claimed the same pair, so a racing
// duplicate sees the committed first answer (or claims it itself when the
// first transaction rolled back). The retention twists it (ADR 0005,
// addendum 2026-11-22): the conflict arm is a DO UPDATE with the age
// predicate as its WHERE — a LIVE row matches nothing (0 rows affected, the
// stored record is read back and replayed), an EXPIRED row is refreshed in
// place (new fingerprint, answer cleared, created_at = now()) and the
// request EXECUTES: past the window the key is free again. The predicate is
// re-checked under the row lock, so a purge that deletes an expired row just
// before the claim only makes the refresh a plain INSERT.
func (s *IdempotencyStore) Claim(ctx context.Context, operation, key, fingerprint string) (idempotency.Record, bool, error) {
	q := queryFor(ctx, s.pool)
	tag, err := q.Exec(ctx, `
		INSERT INTO idempotency_records (operation, key, fingerprint) VALUES ($1, $2, $3)
		ON CONFLICT (operation, key) DO UPDATE SET
			fingerprint = EXCLUDED.fingerprint, answer = NULL, created_at = now()
		WHERE idempotency_records.created_at <= now() - make_interval(secs => $4)`,
		operation, key, fingerprint, ttlSeconds())
	if err != nil {
		return idempotency.Record{}, false, err
	}
	if tag.RowsAffected() == 1 {
		return idempotency.Record{}, true, nil
	}
	var (
		rec idempotency.Record
		raw []byte
	)
	// The age guard on the replay read: only a row still inside the window
	// can answer (the complement of the refresh predicate above, over the
	// one idempotency.RetentionTTL).
	err = q.QueryRow(ctx, `
		SELECT fingerprint, answer FROM idempotency_records
		WHERE operation = $1 AND key = $2 AND created_at > now() - make_interval(secs => $3)`,
		operation, key, ttlSeconds()).Scan(&rec.Fingerprint, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return idempotency.Record{}, false, ports.ErrNotFound
	}
	if err != nil {
		return idempotency.Record{}, false, err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &rec.Answer); err != nil {
			return idempotency.Record{}, false, err
		}
	}
	return rec, false, nil
}

// Complete implements ports.IdempotencyStore.
func (s *IdempotencyStore) Complete(ctx context.Context, operation, key string, answer views.Answer) error {
	raw, err := json.Marshal(answer)
	if err != nil {
		return err
	}
	_, err = queryFor(ctx, s.pool).Exec(ctx, `UPDATE idempotency_records SET answer = $3 WHERE operation = $1 AND key = $2`, operation, key, raw)
	return err
}

// PurgeExpired implements ports.IdempotencyStore: it deletes at most limit
// rows older than idempotency.RetentionTTL, oldest first (the
// idempotency_records_created_at index, migration 0002), and returns how
// many it deleted. FOR UPDATE SKIP LOCKED leaves a row another transaction
// holds — a request whose claim is refreshing an expired row — and the age
// test is repeated on the DELETE itself, so a row a request just refreshed
// is never deleted. It takes no advisory lock and never waits on a row
// lock: a row it cannot take is skipped, not blocked on; an expired row is
// invisible to Claim's replay read anyway, so deleting it changes nothing
// a request can observe.
func (s *IdempotencyStore) PurgeExpired(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM idempotency_records WHERE (operation, key) IN (
			SELECT operation, key FROM idempotency_records
			WHERE created_at <= now() - make_interval(secs => $1)
			ORDER BY created_at LIMIT $2 FOR UPDATE SKIP LOCKED
		) AND created_at <= now() - make_interval(secs => $1)`, ttlSeconds(), limit)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
