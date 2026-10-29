// Package memory holds the in-memory test doubles of the application ports.
// They are for unit and BDD tests only: cmd/** never imports this package
// (internal/architecture's TestRuntimeBinaryHasNoInMemoryAdapters), the
// shipped binary runs on real Postgres. Aggregates round-trip through
// Snapshot/Restore, so a double behaves like a database row.
package memory

import (
	"context"
	"maps"
	"sync"

	"github.com/IQVO/payment-provider-simulator/internal/application/idempotency"
	"github.com/IQVO/payment-provider-simulator/internal/application/ports"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providerauth"
)

// Store is the shared state behind the memory repositories and the unit of
// work. The zero value is not usable; use NewStore.
type Store struct {
	mu    sync.Mutex
	auths map[providerauth.ID]providerauth.Snapshot
	idem  map[string]idempotency.Record
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{auths: map[providerauth.ID]providerauth.Snapshot{}, idem: map[string]idempotency.Record{}}
}

type heldKey struct{}

// locked runs fn with the store mutex held, unless ctx already carries a unit
// of work (which holds it).
func (s *Store) locked(ctx context.Context, fn func()) {
	if ctx.Value(heldKey{}) != nil {
		fn()
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fn()
}

// UnitOfWork is the memory ports.UnitOfWork: it serializes units and restores
// the previous state when fn fails.
type UnitOfWork struct{ s *Store }

var _ ports.UnitOfWork = (*UnitOfWork)(nil)

// NewUnitOfWork returns the unit of work of s.
func NewUnitOfWork(s *Store) *UnitOfWork { return &UnitOfWork{s: s} }

// Do implements ports.UnitOfWork.
func (u *UnitOfWork) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if ctx.Value(heldKey{}) != nil {
		return fn(ctx)
	}
	u.s.mu.Lock()
	defer u.s.mu.Unlock()
	auths, idem := maps.Clone(u.s.auths), maps.Clone(u.s.idem)
	if err := fn(context.WithValue(ctx, heldKey{}, true)); err != nil {
		u.s.auths, u.s.idem = auths, idem
		return err
	}
	return nil
}
