package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/postgres/pgtx"
	"github.com/IQVO/payment-provider-simulator/internal/application/ports"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providerauth"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providertoken"
)

// AuthorizationRepository is the Postgres ports.AuthorizationRepository.
type AuthorizationRepository struct{ pool *pgxpool.Pool }

var _ ports.AuthorizationRepository = (*AuthorizationRepository)(nil)

// NewAuthorizationRepository constructs the repository over pool.
func NewAuthorizationRepository(pool *pgxpool.Pool) *AuthorizationRepository {
	return &AuthorizationRepository{pool: pool}
}

const selectAuthorization = `
	SELECT hold_id, scenario, reference, currency, held_minor, captured_minor, refunded_minor, status,
	       expires_at, created_at, coalesce(superseded_by, ''), movements
	FROM provider_authorizations WHERE hold_id = $1`

type movementJSON struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	AmountMinor int64     `json:"amountMinor"`
	At          time.Time `json:"at"`
}

// Get implements ports.AuthorizationRepository; inside a unit of work the row
// is locked (FOR UPDATE) until the unit ends.
func (r *AuthorizationRepository) Get(ctx context.Context, id providerauth.ID) (*providerauth.Authorization, error) {
	query := selectAuthorization
	if _, inTx := pgtx.From(ctx); inTx {
		query += " FOR UPDATE"
	}
	var (
		s        providerauth.Snapshot
		scenario string
		status   string
		raw      []byte
		super    string
	)
	err := queryFor(ctx, r.pool).QueryRow(ctx, query, string(id)).Scan(
		&s.ID, &scenario, &s.Reference, &s.Currency, &s.HeldMinor, &s.CapturedMinor, &s.RefundedMinor, &status,
		&s.ExpiresAt, &s.CreatedAt, &super, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("authorization %q: %w", id, ports.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	s.Scenario, s.Status, s.SupersededBy = providertoken.Scenario(scenario), providerauth.Status(status), providerauth.ID(super)
	s.ExpiresAt, s.CreatedAt = s.ExpiresAt.UTC(), s.CreatedAt.UTC()
	if s.Movements, err = decodeMovements(raw); err != nil {
		return nil, err
	}
	return providerauth.Restore(s), nil
}

func decodeMovements(raw []byte) ([]providerauth.Movement, error) {
	var in []movementJSON
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("decode movements: %w", err)
	}
	out := make([]providerauth.Movement, 0, len(in))
	for _, m := range in {
		out = append(out, providerauth.Movement{ID: m.ID, Kind: providerauth.MovementKind(m.Kind), AmountMinor: m.AmountMinor, At: m.At.UTC()})
	}
	return out, nil
}

func encodeMovements(ms []providerauth.Movement) ([]byte, error) {
	out := make([]movementJSON, 0, len(ms))
	for _, m := range ms {
		out = append(out, movementJSON{ID: m.ID, Kind: string(m.Kind), AmountMinor: m.AmountMinor, At: m.At})
	}
	return json.Marshal(out)
}

const upsertAuthorization = `
	INSERT INTO provider_authorizations (hold_id, scenario, reference, currency, held_minor, captured_minor,
	       refunded_minor, status, expires_at, created_at, superseded_by, movements)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, nullif($11, ''), $12)
	ON CONFLICT (hold_id) DO UPDATE SET held_minor = EXCLUDED.held_minor, captured_minor = EXCLUDED.captured_minor,
	       refunded_minor = EXCLUDED.refunded_minor, status = EXCLUDED.status, expires_at = EXCLUDED.expires_at,
	       superseded_by = EXCLUDED.superseded_by, movements = EXCLUDED.movements`

// Save implements ports.AuthorizationRepository.
func (r *AuthorizationRepository) Save(ctx context.Context, a *providerauth.Authorization) error {
	s := a.Snapshot()
	movements, err := encodeMovements(s.Movements)
	if err != nil {
		return err
	}
	_, err = queryFor(ctx, r.pool).Exec(ctx, upsertAuthorization, string(s.ID), string(s.Scenario), s.Reference, s.Currency,
		s.HeldMinor, s.CapturedMinor, s.RefundedMinor, string(s.Status), s.ExpiresAt, s.CreatedAt, string(s.SupersededBy), movements)
	return err
}
