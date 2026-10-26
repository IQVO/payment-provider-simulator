package providertoken_test

import (
	"errors"
	"testing"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/domain/providertoken"
	"github.com/IQVO/payment-provider-simulator/internal/domain/shared"
)

func TestParseScenario(t *testing.T) {
	for _, sc := range providertoken.All() {
		got, err := providertoken.ParseScenario(string(sc))
		if err != nil || got != sc {
			t.Fatalf("ParseScenario(%q) = %q, %v", sc, got, err)
		}
	}
	if got, err := providertoken.ParseScenario(""); err != nil || got != providertoken.Approve {
		t.Fatalf("empty scenario = %q, %v; want approve", got, err)
	}
	if _, err := providertoken.ParseScenario("bogus"); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("bogus scenario err = %v; want validation", err)
	}
}

func TestIssueAndScenarioOfRoundTrip(t *testing.T) {
	for _, sc := range providertoken.All() {
		for _, suffix := range []string{"", "a1b2c3"} {
			got, err := providertoken.ScenarioOf(providertoken.Issue(sc, suffix))
			if err != nil || got != sc {
				t.Fatalf("round trip %q/%q = %q, %v", sc, suffix, got, err)
			}
		}
	}
	if got := providertoken.Issue(providertoken.Decline, "x"); got != "tok_decline_x" {
		t.Fatalf("Issue = %q", got)
	}
	if got := providertoken.Issue(providertoken.Decline, ""); got != "tok_decline" {
		t.Fatalf("Issue without suffix = %q", got)
	}
}

func TestScenarioOfRejects(t *testing.T) {
	for _, token := range []string{"", "approve", "tok_", "tok_approved", "tok_declinex", "xtok_approve", "pm_123"} {
		_, err := providertoken.ScenarioOf(token)
		if !errors.Is(err, providertoken.ErrUnknownToken) || !errors.Is(err, shared.ErrValidation) {
			t.Errorf("ScenarioOf(%q) err = %v; want unknown token", token, err)
		}
	}
}

func TestScenarioOfRefusesCardNumbers(t *testing.T) {
	for _, pan := range []string{"4111111111111111", "4111 1111 1111 1111", "4111-1111-1111-1111", "4111111111111", "4111111111111111111"} {
		_, err := providertoken.ScenarioOf(pan)
		if !errors.Is(err, shared.ErrValidation) || errors.Is(err, providertoken.ErrUnknownToken) {
			t.Errorf("ScenarioOf(%q) err = %v; want card-number validation", pan, err)
		}
	}
	for _, short := range []string{"411111111111", "41111111111111111111", "12ab34"} {
		if _, err := providertoken.ScenarioOf(short); !errors.Is(err, providertoken.ErrUnknownToken) {
			t.Errorf("ScenarioOf(%q) err = %v; want unknown token", short, err)
		}
	}
}

func TestHoldValidity(t *testing.T) {
	if got := providertoken.ExpireEarly.HoldValidity(); got != 5*time.Minute {
		t.Fatalf("expire-early validity = %v", got)
	}
	for _, sc := range []providertoken.Scenario{providertoken.Approve, providertoken.Decline, providertoken.Timeout} {
		if got := sc.HoldValidity(); got != 7*24*time.Hour {
			t.Fatalf("%s validity = %v", sc, got)
		}
	}
}

func TestCardNumberDigitBoundaries(t *testing.T) {
	for _, pan := range []string{"0000000000000", "9999999999999", "0123456789 0123", "0123456789-0123"} {
		if _, err := providertoken.ScenarioOf(pan); errors.Is(err, providertoken.ErrUnknownToken) {
			t.Errorf("%q must be refused as a card number", pan)
		}
	}
	for _, notPan := range []string{"/////////////", ":::::::::::::", "0000000000000x", "!!!!!!!!!!!!!"} {
		if _, err := providertoken.ScenarioOf(notPan); !errors.Is(err, providertoken.ErrUnknownToken) {
			t.Errorf("%q is not a card number, got %v", notPan, err)
		}
	}
}
