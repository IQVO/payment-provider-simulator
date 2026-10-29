package memory

import (
	"context"
	"fmt"

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

// Claim implements ports.IdempotencyStore.
func (i *Idempotency) Claim(ctx context.Context, operation, key, fingerprint string) (idempotency.Record, bool, error) {
	var (
		rec     idempotency.Record
		claimed bool
	)
	i.s.locked(ctx, func() {
		k := idemKey(operation, key)
		if existing, ok := i.s.idem[k]; ok {
			rec = existing
			return
		}
		i.s.idem[k] = idempotency.Record{Fingerprint: fingerprint}
		claimed = true
	})
	return rec, claimed, nil
}

// Complete implements ports.IdempotencyStore.
func (i *Idempotency) Complete(ctx context.Context, operation, key string, answer views.Answer) error {
	i.s.locked(ctx, func() {
		k := idemKey(operation, key)
		rec := i.s.idem[k]
		rec.Answer = answer
		i.s.idem[k] = rec
	})
	return nil
}
