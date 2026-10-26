package ids_test

import (
	"regexp"
	"testing"

	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/ids"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providerauth"
)

func TestIDShapesAndUniqueness(t *testing.T) {
	g := ids.Generator{}
	checks := map[string]*regexp.Regexp{
		string(g.NewHoldID()):                     regexp.MustCompile(`^hold_[0-9a-f]{12}$`),
		g.NewMovementID(providerauth.KindCapture): regexp.MustCompile(`^capture_[0-9a-f]{12}$`),
		g.NewMovementID(providerauth.KindRefund):  regexp.MustCompile(`^refund_[0-9a-f]{12}$`),
		g.NewTokenSuffix():                        regexp.MustCompile(`^[0-9a-f]{12}$`),
	}
	for id, re := range checks {
		if !re.MatchString(id) {
			t.Errorf("%q does not match %s", id, re)
		}
	}
	first, second := g.NewHoldID(), g.NewHoldID()
	if first == second {
		t.Fatal("ids must be unique")
	}
}
