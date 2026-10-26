// Package providertoken is the vocabulary of the simulator's payment-method
// tokens. A token names its test scenario (tok_<scenario>[_<suffix>]), so a
// token always behaves the same way: the storefront's saga tests are
// reproducible. Only tokens travel; a card number is refused.
package providertoken

import (
	"errors"
	"strings"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/domain/shared"
)

// Scenario is the deterministic behaviour a token selects.
type Scenario string

// The scenario vocabulary (documented in the README and apis/openapi.yaml).
const (
	// Approve authorizes normally; the hold lasts DefaultHoldValidity.
	Approve Scenario = "approve"
	// Decline refuses every authorization with DeclineCode (final, 4xx).
	Decline Scenario = "decline"
	// Timeout answers an authorization 504 after a short delay (transient, 5xx).
	Timeout Scenario = "timeout"
	// ExpireEarly authorizes, but the hold lasts only ExpireEarlyValidity.
	ExpireEarly Scenario = "expire-early"
)

const (
	// DefaultHoldValidity is how long an approved hold lasts.
	DefaultHoldValidity = 7 * 24 * time.Hour
	// ExpireEarlyValidity is how long an expire-early hold lasts.
	ExpireEarlyValidity = 5 * time.Minute
	// DeclineCode is the code of a declined authorization.
	DeclineCode = "CARD_DECLINED"

	prefix = "tok_"
)

// ErrUnknownToken: the token names no known scenario.
var ErrUnknownToken = errors.New("unknown payment-method token")

// All lists the scenarios in documentation order.
func All() []Scenario { return []Scenario{Approve, Decline, Timeout, ExpireEarly} }

// ParseScenario returns the scenario named s; an empty s means Approve.
func ParseScenario(s string) (Scenario, error) {
	if s == "" {
		return Approve, nil
	}
	for _, sc := range All() {
		if string(sc) == s {
			return sc, nil
		}
	}
	return "", shared.Validation("scenario %q is not one of approve, decline, timeout, expire-early", s)
}

// Issue builds the token of a scenario with a unique suffix.
func Issue(s Scenario, suffix string) string {
	if suffix == "" {
		return prefix + string(s)
	}
	return prefix + string(s) + "_" + suffix
}

// ScenarioOf reads the scenario a token names. A card number is a validation
// error, any other unrecognised string is ErrUnknownToken wrapped in one.
func ScenarioOf(token string) (Scenario, error) {
	if looksLikeCardNumber(token) {
		return "", shared.Validation("a card number is never accepted, only a payment-method token")
	}
	rest, ok := strings.CutPrefix(token, prefix)
	if ok {
		for _, sc := range All() {
			if rest == string(sc) || strings.HasPrefix(rest, string(sc)+"_") {
				return sc, nil
			}
		}
	}
	return "", errors.Join(shared.ErrValidation, ErrUnknownToken)
}

// HoldValidity is how long a hold authorized under the scenario lasts.
func (s Scenario) HoldValidity() time.Duration {
	if s == ExpireEarly {
		return ExpireEarlyValidity
	}
	return DefaultHoldValidity
}

func looksLikeCardNumber(token string) bool {
	digits := 0
	for _, r := range token {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == ' ' || r == '-':
		default:
			return false
		}
	}
	return digits >= 13 && digits <= 19
}
