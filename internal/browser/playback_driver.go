package browser

import (
	"context"
	"sync"

	"mistervision/internal/media"
	"mistervision/internal/playback"
	"mistervision/internal/sound"
	"mistervision/internal/videoout"
)

// playbackDriver connects the controller to external decoding. Launch runs on
// the browser loop. Decoder callbacks send values back through events, never
// touching browser state. The controller owns the returned process lifecycle.
type playbackDriver struct {
	ctx      context.Context
	feedback sound.Feedback
	config   playback.Config
	output   videoout.Output
	events   chan PlaybackEvent
	sequence int
	cleanup  sync.WaitGroup // Application shutdown joins detached server cleanup.
}

func (d *playbackDriver) send(event PlaybackEvent) {
	select {
	case d.events <- event:
	case <-d.ctx.Done():
	}
}

func (d *playbackDriver) launch(client media.Playback, item media.Item, offset *int64, gate <-chan struct{}, prepared bool, controls chan playback.Control, tracks playback.TrackOptions) playbackProcess {
	d.sequence++
	id := d.sequence
	ctx, stop := context.WithCancel(d.ctx)
	finished := make(chan struct{})
	cleanup := make(chan struct{})
	request := playback.Request{
		Item: item, StartTicks: offset, Start: gate,
		AsyncCleanup: cleanup, Controls: controls,
	}
	request.Callbacks.Levels = func(levels playback.AudioLevels) {
		select {
		case d.events <- PlaybackEvent{Kind: PlaybackLevels, ID: id, Levels: levels}:
		default:
		}
	}
	// New playback restores per-item choices. Replacements carry the current
	// controller choices so seeking never reloads an older saved selection.
	if prepared {
		request.Tracks = &tracks
	}
	request.Callbacks.Caption = func(text string) {
		d.send(PlaybackEvent{Kind: PlaybackCaption, ID: id, Caption: text})
	}
	request.Callbacks.Picture = func(result playback.PictureResult) {
		d.send(PlaybackEvent{Kind: PlaybackPicture, ID: id, Picture: result})
	}
	request.Callbacks.TrackInfo = func(info playback.VideoTracks) { d.send(PlaybackEvent{Kind: PlaybackTrackInfo, ID: id, Tracks: info}) }
	request.Callbacks.Subtitle = func(result playback.SubtitleResult) {
		d.send(PlaybackEvent{Kind: PlaybackSubtitle, ID: id, Subtitle: result})
	}
	d.cleanup.Add(1)
	request.Callbacks.CleanupDone = func() {
		defer d.cleanup.Done()
		d.send(PlaybackEvent{Kind: PlaybackCleanupDone, ID: id})
	}
	request.Callbacks.AcquireVideo = d.output.Acquire
	request.Callbacks.ReleaseVideo = d.output.Release
	request.Callbacks.ControlError = func(err error) { d.send(PlaybackEvent{Kind: PlaybackControlFailed, ID: id, Err: err}) }
	request.Callbacks.Paused = func(paused bool) { d.send(PlaybackEvent{Kind: PlaybackPaused, ID: id, Value: paused}) }
	request.Callbacks.Buffering = func(waiting bool) { d.send(PlaybackEvent{Kind: PlaybackBuffering, ID: id, Value: waiting}) }
	request.Callbacks.VideoStarted = func() { d.send(PlaybackEvent{Kind: PlaybackVideoStarted, ID: id}) }
	if prepared {
		request.Callbacks.Ready = func() { d.send(PlaybackEvent{Kind: PlaybackPrepared, ID: id}) }
	}
	request.Callbacks.Position = func(ticks int64) {
		// Progress is disposable. A full queue must not stall the decoder.
		select {
		case d.events <- PlaybackEvent{Kind: PlaybackPosition, ID: id, Ticks: ticks}:
		default:
		}
	}
	go func() {
		defer close(finished)
		resumeSounds := func() {}
		if d.feedback != nil {
			resumeSounds = d.feedback.Suspend()
		}
		defer resumeSounds()
		err := playback.Run(ctx, client, d.config, request)
		resumeSounds()
		d.send(PlaybackEvent{Kind: PlaybackEnded, ID: id, Err: err})
	}()
	return playbackProcess{id: id, cancel: stop, done: finished, cleanup: cleanup}
}
