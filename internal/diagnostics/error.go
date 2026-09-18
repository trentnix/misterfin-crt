package diagnostics

import (
	"context"
	"errors"
	"os"
	"syscall"

	"mistervision/internal/media"
)

// ErrorKind classifies a failure without exposing its message, paths, or payload.
// Unknown errors remain unclassified instead of falling back to error text.
func ErrorKind(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err):
		return "timeout"
	case errors.Is(err, media.ErrUnauthorized):
		return "unauthorized"
	case errors.Is(err, media.ErrUnavailable):
		return "unavailable"
	case errors.Is(err, media.ErrNotFound):
		return "not-found"
	case errors.Is(err, media.ErrTuning):
		return "tuning"
	case errors.Is(err, media.ErrConversion):
		return "conversion"
	case errors.Is(err, media.ErrServerFailure):
		return "server"
	case errors.Is(err, syscall.ENOSPC):
		return "storage-full"
	case errors.Is(err, syscall.EROFS):
		return "read-only"
	case errors.Is(err, os.ErrPermission):
		return "permission"
	case errors.Is(err, os.ErrNotExist):
		return "not-found"
	default:
		return "other"
	}
}
