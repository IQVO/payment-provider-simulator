// Package ids is the production ports.IDGenerator: every id is a prefix and 12
// random hexadecimal characters.
package ids

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/IQVO/payment-provider-simulator/internal/application/ports"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providerauth"
)

// Generator mints ids.
type Generator struct{}

var _ ports.IDGenerator = Generator{}

func random(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("ids: system randomness is unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// NewHoldID returns "hold_" and 12 hexadecimal characters.
func (Generator) NewHoldID() providerauth.ID { return providerauth.ID("hold_" + random(6)) }

// NewMovementID returns "capture_" or "refund_" and 12 hexadecimal characters.
func (Generator) NewMovementID(kind providerauth.MovementKind) string {
	return strings.ToLower(string(kind)) + "_" + random(6)
}

// NewTokenSuffix returns 12 hexadecimal characters.
func (Generator) NewTokenSuffix() string { return random(6) }
