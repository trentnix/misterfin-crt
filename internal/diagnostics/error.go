package diagnostics

import (
	"context"
	"errors"
	"os"
)

// ErrorKind classifies a failure without exposing its message, paths, or payload.
// Unknown errors remain unclassified instead of falling back to error text.
func ErrorKind(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, os.ErrPermission):
		return "permission"
	case errors.Is(err, os.ErrNotExist):
		return "not-found"
	default:
		return "other"
	}
}
