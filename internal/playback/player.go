// Package playback runs an external video player and owns its Jellyfin session.
package playback

import (
	"context"
	"errors"

	"misterfin-crt/internal/jellyfin"
	playerapi "misterfin-crt/internal/player"
)

// Run prepares a session and stream, waits for the controller's start gate, and
// runs the decoder. Teardown reaps the decoder and stops stream copying before
// releasing video output, closing the source, and reporting the final position.
//
// Canceling ctx stops preparation or playback. Cancellation during preparation
// or monitoring returns nil. Setup errors can still be returned during cancellation.
// With Request.AsyncCleanup enabled, reporting and tuner release may outlive Run.
// Returned errors exclude stream URLs and raw decoder diagnostics.
func Run(ctx context.Context, c *jellyfin.Client, config Config, request Request) (resultErr error) {
	var trace *playbackTrace
	if c != nil {
		trace = newPlaybackTrace(c.Diagnostics, config, request)
	}
	defer func() { trace.finish(ctx, resultErr) }()
	trace.phase("decoder-configuration")
	cleanupOwned := false
	defer func() {
		if !cleanupOwned && request.Callbacks.CleanupDone != nil {
			request.Callbacks.CleanupDone()
		}
	}()
	choices := prepareTrackChoices(c, config, request)
	decoder, executable, err := resolveDecoder(config, request.Item, choices.picture())
	if err != nil {
		return err
	}
	decoder, meter := configureAudioLevels(decoder, request.Item.Type == "Audio" && request.Callbacks.Levels != nil)
	if meter != nil {
		defer meter.Close()
	}
	choices.clientSubtitles = decoder.ClientSubtitles()
	_, choices.livePicture = decoder.(playerapi.PictureSetter)
	trace.phase("metadata")
	session, err := preparePlayback(ctx, c, config, request, choices)
	if err != nil || session == nil {
		return err
	}
	_, audioSeekable := decoder.(playerapi.AudioSeeker)
	canSeek := !session.liveTV && (session.item.Type != "Audio" || audioSeekable)
	session.state.CanSeek = &canSeek
	session.trace = trace
	trace.prepared(session)
	mediaCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	session.meter = meter
	session.reporter = newProgressReporter(ctx, c, session.liveTV)
	cleanupOwned = true
	defer func() {
		failed := session.finish(resultErr != nil, request.AsyncCleanup, request.Callbacks.CleanupDone)
		if resultErr == nil && ctx.Err() == nil && failed {
			resultErr = errors.New("playback ended, but Jellyfin progress reporting failed")
		}
	}()
	if request.Callbacks.TrackInfo != nil && session.item.Type != "Audio" {
		request.Callbacks.TrackInfo(session.tracks)
	}
	trace.phase("stream-open")
	source, err := openMedia(mediaCtx, c, session.streamURL, decoder.Input(session.item))
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	defer source.close()
	if request.Callbacks.Ready != nil {
		request.Callbacks.Ready()
	}
	trace.phase("start-gate")
	if request.Start != nil {
		select {
		case <-request.Start:
		case <-ctx.Done():
			return nil
		}
	}
	trace.phase("decoder-start")
	process, err := startProcess(mediaCtx, executable, decoder.Args(session.item, source.url), source, decoder)
	if err != nil {
		return err
	}
	if session.item.Type != "Audio" && request.Callbacks.AcquireVideo != nil {
		request.Callbacks.AcquireVideo()
		if request.Callbacks.ReleaseVideo != nil {
			defer request.Callbacks.ReleaseVideo()
		}
	}
	trace.phase("playing")
	process.feed()
	defer func() { cancel(); process.close() }()
	return session.monitor(ctx, cancel, process, request)
}
