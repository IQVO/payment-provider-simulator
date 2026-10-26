package shared_test

import (
	"errors"
	"testing"

	"github.com/IQVO/payment-provider-simulator/internal/domain/shared"
)

func TestHelpersWrapTheSentinels(t *testing.T) {
	v := shared.Validation("bad %d", 1)
	if !errors.Is(v, shared.ErrValidation) || v.Error() != "validation failed: bad 1" {
		t.Fatalf("Validation = %v", v)
	}
	i := shared.Illegal("no %s", "way")
	if !errors.Is(i, shared.ErrIllegalTransition) || i.Error() != "illegal transition: no way" {
		t.Fatalf("Illegal = %v", i)
	}
}
