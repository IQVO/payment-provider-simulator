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

// IdempotencyStore is the Postgres ports.IdempotencyStore.
type IdempotencyStore struct{ pool *pgxpool.Pool }

var _ ports.IdempotencyStore = (*IdempotencyStore)(nil)

// NewIdempotencyStore constructs the store over pool.
func NewIdempotencyStore(pool *pgxpool.Pool) *IdempotencyStore { return &IdempotencyStore{pool: pool} }

// Claim implements ports.IdempotencyStore. The INSERT .. ON CONFLICT DO
// NOTHING waits for a concurrent transaction that claimed the same pair, so a
// racing duplicate sees the committed first answer (or claims it itself when
// the first transaction rolled back).
func (s *IdempotencyStore) Claim(ctx context.Context, operation, key, fingerprint string) (idempotency.Record, bool, error) {
	q := queryFor(ctx, s.pool)
	tag, err := q.Exec(ctx, `
		INSERT INTO idempotency_records (operation, key, fingerprint) VALUES ($1, $2, $3)
		ON CONFLICT (operation, key) DO NOTHING`, operation, key, fingerprint)
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
	err = q.QueryRow(ctx, `SELECT fingerprint, answer FROM idempotency_records WHERE operation = $1 AND key = $2`, operation, key).Scan(&rec.Fingerprint, &raw)
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
