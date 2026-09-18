package media

import (
	"context"
	"errors"
	"os"
)

// ErrUnauthorized identifies explicit credential rejection. Transport failures
// must not match it, because temporary failures must retain saved sign-in.
var ErrUnauthorized = errors.New("server rejected authentication")

// ErrUnavailable identifies a transport failure without retaining a private URL.
var ErrUnavailable = errors.New("media server is unavailable")

// ErrNotFound means the requested media or library no longer exists on the server.
var ErrNotFound = errors.New("media was not found")

// ErrServerFailure identifies a server-side HTTP failure.
var ErrServerFailure = errors.New("media server request failed")

// NetworkError preserves cancellation and timeouts while removing request URLs
// and credentials from transport errors. Call it only for failed network I/O.
func NetworkError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
		return errors.Join(ErrUnavailable, context.DeadlineExceeded)
	}
	return ErrUnavailable
}

// ErrConversion means the server could not prepare the requested video format.
var ErrConversion = errors.New("server could not convert video")

// ErrTuning means the server could not provide a playable Live TV channel.
var ErrTuning = errors.New("server could not tune channel")
