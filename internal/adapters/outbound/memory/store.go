// Package memory holds the in-memory test doubles of the application ports.
// They are for unit and BDD tests only: cmd/** never imports this package
// (internal/architecture's TestRuntimeBinaryHasNoInMemoryAdapters), the
// shipped binary runs on real Postgres. Aggregates round-trip through
// Snapshot/Restore, so a double behaves like a database row.
package memory

import (
	"context"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/application/idempotency"
	"github.com/IQVO/payment-provider-simulator/internal/application/ports"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providerauth"
)

// idemEntry is one idempotency record plus the instant it was claimed (the
// double of idempotency_records.created_at, against which the retention
// window is measured).
type idemEntry struct {
	rec       idempotency.Record
	claimedAt time.Time
}

// Store is the shared state behind the memory repositories and the unit of
// work. The zero value is not usable; use NewStore.
type Store struct {
	mu    sync.Mutex
	auths map[providerauth.ID]providerauth.Snapshot
	idem  map[string]idemEntry
	now   func() time.Time
}

// NewStore returns an empty store stamping claims with the wall clock (a
// record claimed "now" is live for the whole idempotency.RetentionTTL).
func NewStore() *Store {
	return newStore(time.Now)
}

// NewStoreClock returns an empty store stamping claims with clock — the
// same settable Clock the use cases read, so a test that advances it
// advances the retention window with it.
func NewStoreClock(clock *Clock) *Store {
	return newStore(clock.Now)
}

func newStore(now func() time.Time) *Store {
	return &Store{
		auths: map[providerauth.ID]providerauth.Snapshot{},
		idem:  map[string]idemEntry{},
		now:   now,
	}
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

// Do implements ports.UnitOfWork. It restores the previous state when fn
// fails — or PANICS: a panic aborts the unit like the Postgres transaction it
// stands for, so a claimed key never survives a panicked effect (the fleet
// rule: a panic is never stored).
func (u *UnitOfWork) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if ctx.Value(heldKey{}) != nil {
		return fn(ctx)
	}
	u.s.mu.Lock()
	defer u.s.mu.Unlock()
	auths, idem := maps.Clone(u.s.auths), maps.Clone(u.s.idem)
	committed := false
	defer func() {
		if !committed {
			u.s.auths, u.s.idem = auths, idem
		}
	}()
	if err := fn(context.WithValue(ctx, heldKey{}, true)); err != nil {
		return err
	}
	committed = true
	return nil
}

// Held is how many idempotency records the store holds for key across all
// operations (0 after the key was released). Test accessor for the replay-class
// reference test, which must not know the store's shape.
func (s *Store) Held(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for k := range s.idem {
		if _, held, ok := strings.Cut(k, "|"); ok && held == key {
			n++
		}
	}
	return n
}
