package browser

import (
	"context"
	"errors"
	"fmt"
	"time"

	"mistervision/internal/media"
	"mistervision/internal/playback"
	"mistervision/internal/rendering"
)

// browserMessage keeps playback lifetime policy outside the render presentation.
// Reporting warnings survive track changes until their original deadline.
type browserMessage struct {
	rendering.MessagePresentation
	preserveOnPlayback bool
}

// requestFailure explains a failed operation without displaying raw provider
// errors, response bodies, or URLs. The caller names the failed operation.
func requestFailure(operation string, err error) string {
	guidance := messageRetry
	switch {
	case errors.Is(err, media.ErrUnauthorized):
		guidance = messageSignInAgain
	case errors.Is(err, media.ErrTuning):
		guidance = messageTunerUnavailable
	case errors.Is(err, media.ErrConversion):
		guidance = messageConversionFailed
	case errors.Is(err, context.DeadlineExceeded):
		guidance = messageRequestTimeout
	case errors.Is(err, media.ErrUnavailable):
		guidance = messageServerUnavailable
	case errors.Is(err, media.ErrNotFound):
		guidance = messageItemMissing
	case errors.Is(err, media.ErrServerFailure):
		guidance = messageServerFailed
	}
	return operation + " " + guidance
}

// showPlaybackError uses the same recovery presentation for local and remote
// playback. A fresh attempt clears failures, but reporting warnings retain their deadline.
func (s *browserSession) showPlaybackError(err error) {
	header, text := titlePlaybackNotStarted, requestFailure(messageStreamFailed, err)
	switch {
	case errors.Is(err, playback.ErrPlayerUnavailable):
		text = messagePlayerUnavailable
	case errors.Is(err, playback.ErrTrackUnavailable):
		text = messageTrackUnavailable
	case errors.Is(err, playback.ErrStartupTimeout):
		text = messageStartupTimeout
	case errors.Is(err, playback.ErrNotStarted):
		text = messagePlayerNotStarted
	case errors.Is(err, playback.ErrInterrupted):
		header, text = titlePlaybackInterrupted, messagePlaybackInterrupted
	case errors.Is(err, playback.ErrProgress):
		header, text = titleProgressFailed, messageProgressFailed
	}
	s.message = browserMessage{MessagePresentation: rendering.MessagePresentation{Header: header, Text: text, Until: time.Now().Add(8 * time.Second)}, preserveOnPlayback: errors.Is(err, playback.ErrProgress)}
}

// neighborFailure uses the requested direction rather than navigation terminology.
func neighborFailure(direction int, kind string) string {
	which := "next"
	if direction < 0 {
		which = "previous"
	}
	return fmt.Sprintf(messageNeighborFailed, which, kind)
}
