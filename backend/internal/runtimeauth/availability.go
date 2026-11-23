package runtimeauth

import (
	"context"
	"errors"
)

// DependencyFailure distinguishes unavailable authority from a negative
// authorization decision. Callers must still deny access; only renewal may retry.
type DependencyFailure struct{ cause error }

func (e *DependencyFailure) Error() string { return "runtime authority temporarily unavailable" }
func (e *DependencyFailure) Unwrap() error { return e.cause }

func Unavailable(err error) error {
	if err == nil {
		return nil
	}
	if IsUnavailable(err) {
		return err
	}
	return &DependencyFailure{cause: err}
}

func IsUnavailable(err error) bool {
	var dependency *DependencyFailure
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.As(err, &dependency)
}
