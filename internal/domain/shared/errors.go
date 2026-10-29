// Package shared holds the error vocabulary every domain package uses, so the
// adapters can map a failure to a problem type without importing each aggregate.
package shared

import (
	"errors"
	"fmt"
)

var (
	// ErrValidation marks input the domain refuses (problem type validation-failed, 422).
	ErrValidation = errors.New("validation failed")
	// ErrIllegalTransition marks a command the current state does not allow
	// (problem type illegal-transition, 409).
	ErrIllegalTransition = errors.New("illegal transition")
)

// Validation wraps ErrValidation with a reason.
func Validation(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

// Illegal wraps ErrIllegalTransition with a reason.
func Illegal(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrIllegalTransition, fmt.Sprintf(format, args...))
}
