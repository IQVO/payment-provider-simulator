package memory

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/application/ports"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providerauth"
)

// Clock is a settable ports.Clock.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

var _ ports.Clock = (*Clock)(nil)

// NewClock returns a clock reading at.
func NewClock(at time.Time) *Clock { return &Clock{now: at} }

// Now implements ports.Clock.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// IDs is a deterministic ports.IDGenerator: hold-1, capture-2, ...
type IDs struct {
	mu  sync.Mutex
	seq int
}

var _ ports.IDGenerator = (*IDs)(nil)

func (g *IDs) next() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.seq++
	return g.seq
}

// NewHoldID implements ports.IDGenerator.
func (g *IDs) NewHoldID() providerauth.ID { return providerauth.ID(fmt.Sprintf("hold_%d", g.next())) }

// NewMovementID implements ports.IDGenerator.
func (g *IDs) NewMovementID(kind providerauth.MovementKind) string {
	return fmt.Sprintf("%s_%d", strings.ToLower(string(kind)), g.next())
}

// NewTokenSuffix implements ports.IDGenerator.
func (g *IDs) NewTokenSuffix() string { return fmt.Sprintf("t%d", g.next()) }
