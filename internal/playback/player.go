// Package playback runs an external video player and owns its Jellyfin session.
package playback

import (
	"context"

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
	o, executable, err := o.resolve(item)
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
	defer func() { session.finish(o.AsyncCleanup, resultErr != nil) }()
	source, err := openMedia(mediaCtx, c, session.item, session.streamURL, o)
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
	args := o.args(session.item)
	if source.url != "" {
		if o.TerminalPlayer != "" {
			args = append(args, "--source", source.url)
		} else {
			args[len(args)-1] = source.url
		}
	}
	process, err := startProcess(mediaCtx, executable, args, source)
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
	return session.monitor(ctx, mediaCtx, cancel, process, o, position)
}
