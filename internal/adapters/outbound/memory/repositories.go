package memory

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/application/idempotency"
	"github.com/IQVO/payment-provider-simulator/internal/application/ports"
	"github.com/IQVO/payment-provider-simulator/internal/application/views"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providerauth"
)

// Authorizations is the memory ports.AuthorizationRepository.
type Authorizations struct{ s *Store }

var _ ports.AuthorizationRepository = (*Authorizations)(nil)

// NewAuthorizations returns the repository over s.
func NewAuthorizations(s *Store) *Authorizations { return &Authorizations{s: s} }

// Get implements ports.AuthorizationRepository.
func (r *Authorizations) Get(ctx context.Context, id providerauth.ID) (*providerauth.Authorization, error) {
	var (
		snap providerauth.Snapshot
		ok   bool
	)
	r.s.locked(ctx, func() { snap, ok = r.s.auths[id] })
	if !ok {
		return nil, fmt.Errorf("authorization %q: %w", id, ports.ErrNotFound)
	}
	return providerauth.Restore(snap), nil
}

// Save implements ports.AuthorizationRepository.
func (r *Authorizations) Save(ctx context.Context, a *providerauth.Authorization) error {
	r.s.locked(ctx, func() { r.s.auths[a.ID()] = a.Snapshot() })
	return nil
}

// Idempotency is the memory ports.IdempotencyStore.
type Idempotency struct{ s *Store }

var _ ports.IdempotencyStore = (*Idempotency)(nil)

// NewIdempotency returns the store over s.
func NewIdempotency(s *Store) *Idempotency { return &Idempotency{s: s} }

func idemKey(operation, key string) string { return operation + "|" + key }

// live reports whether the entry is still inside the retention window at t
// (the double of the Postgres created_at > now() - TTL guard).
func live(e idemEntry, t time.Time) bool { return t.Sub(e.claimedAt) < idempotency.RetentionTTL }

// Claim implements ports.IdempotencyStore, with the same retention the
// Postgres store enforces (ADR 0005, addendum 2026-11-22): a LIVE record
// replays (or answers the fingerprint mismatch through Once), an EXPIRED
// one is refreshed — overwritten with the new fingerprint and a fresh
// stamp — and the caller claims it: past idempotency.RetentionTTL the key
// is free again.
func (i *Idempotency) Claim(ctx context.Context, operation, key, fingerprint string) (idempotency.Record, bool, error) {
	var (
		rec     idempotency.Record
		claimed bool
	)
	i.s.locked(ctx, func() {
		k := idemKey(operation, key)
		if existing, ok := i.s.idem[k]; ok && live(existing, i.s.now()) {
			rec = existing.rec
			return
		}
		i.s.idem[k] = idemEntry{rec: idempotency.Record{Fingerprint: fingerprint}, claimedAt: i.s.now()}
		claimed = true
	})
	return rec, claimed, nil
}

// Complete implements ports.IdempotencyStore.
func (i *Idempotency) Complete(ctx context.Context, operation, key string, answer views.Answer) error {
	i.s.locked(ctx, func() {
		k := idemKey(operation, key)
		rec := i.s.idem[k]
		rec.rec.Answer = answer
		i.s.idem[k] = rec
	})
	return nil
}

// PurgeExpired implements ports.IdempotencyStore: it deletes at most limit
// records that can no longer answer a replay (age >=
// idempotency.RetentionTTL), oldest first, and returns how many. One mutex
// covers Claim and Complete too, so a purge never touches a record a
// caller is storing under.
func (i *Idempotency) PurgeExpired(ctx context.Context, limit int) (int, error) {
	deleted := 0
	i.s.locked(ctx, func() {
		if limit <= 0 {
			return
		}
		var expired []string
		now := i.s.now()
		for k, e := range i.s.idem {
			if !live(e, now) {
				expired = append(expired, k)
			}
		}
		sort.Slice(expired, func(a, b int) bool { return i.s.idem[expired[a]].claimedAt.Before(i.s.idem[expired[b]].claimedAt) })
		if len(expired) > limit {
			expired = expired[:limit]
		}
		for _, k := range expired {
			delete(i.s.idem, k)
		}
		deleted = len(expired)
	})
	return deleted, nil
}
