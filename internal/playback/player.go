// Package playback runs an external video player and owns its Jellyfin session.
package playback

import (
	"context"
	"errors"

	"misterfin-go/internal/jellyfin"
)

// Run prepares a session and stream, waits for the controller's start gate, and
// runs the decoder. Teardown reaps the decoder and stops stream copying before
// releasing video output, closing the source, and reporting the final position.
//
// The position callback must be non-nil. It receives absolute Jellyfin positions
// in 100-nanosecond ticks synchronously on Run's goroutine and must return promptly.
// Canceling ctx stops preparation or playback. Cancellation during preparation or
// monitoring returns nil. Setup errors can still be returned during cancellation.
// With Options.AsyncCleanup enabled, final server reporting may outlive Run.
// Returned errors exclude stream URLs and raw decoder diagnostics.
func Run(ctx context.Context, c *jellyfin.Client, item jellyfin.Item, o Options, position func(int64)) (resultErr error) {
	decoder, executable, err := resolveDecoder(o, item)
	if err != nil {
		return err
	}
	session, err := preparePlayback(ctx, c, item, o)
	if err != nil || session == nil {
		return err
	}
	defer session.closeLive()
	mediaCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	session.reporter = newProgressReporter(ctx, c, session.liveTV)
	defer func() {
		failed := session.reporter.finish(session.state, session.started, session.played, resultErr != nil, o.AsyncCleanup)
		if resultErr == nil && ctx.Err() == nil && failed {
			resultErr = errors.New("playback ended, but Jellyfin progress reporting failed")
		}
	}()
	source, err := openMedia(mediaCtx, c, session.streamURL, decoder.input(session.item))
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	defer source.close()
	if o.Ready != nil {
		o.Ready()
	}
	if o.Start != nil {
		select {
		case <-o.Start:
		case <-ctx.Done():
			return nil
		}
	}
	process, err := startProcess(mediaCtx, executable, decoder.args(session.item, source.url), source, decoder)
	if err != nil {
		return err
	}
	if session.item.Type != "Audio" && o.AcquireVideo != nil {
		o.AcquireVideo()
		if o.ReleaseVideo != nil {
			defer o.ReleaseVideo()
		}
	}
	process.feed()
	defer func() { cancel(); process.close() }()
	return session.monitor(ctx, cancel, process, o, position)
}
