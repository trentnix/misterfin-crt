package browser

import (
	"context"
	"sync"

	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/playback"
	"misterfin-crt/internal/videoout"
)

// playbackDriver connects the controller to external decoding. Launch runs on
// the browser loop. Decoder callbacks send values back through events, never
// touching browser state. The controller owns the returned process lifecycle.
type playbackDriver struct {
	ctx      context.Context
	options  playback.Options
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

func (d *playbackDriver) launch(client *jellyfin.Client, item jellyfin.Item, offset *int64, gate <-chan struct{}, prepared bool, controls chan playback.Control, tracks playback.TrackOptions) playbackProcess {
	d.sequence++
	id := d.sequence
	ctx, stop := context.WithCancel(d.ctx)
	finished := make(chan struct{})
	cleanup := make(chan struct{})
	options := d.options
	options.Levels = func(levels playback.AudioLevels) {
		select {
		case d.events <- PlaybackEvent{Kind: PlaybackLevels, ID: id, Levels: levels}:
		default:
		}
	}
	// New playback restores per-item choices. Replacements carry the current
	// controller choices so seeking never reloads an older saved selection.
	options.Tracks = nil
	if prepared {
		options.Tracks = &tracks
	}
	options.Picture = func(result playback.PictureResult) {
		d.send(PlaybackEvent{Kind: PlaybackPicture, ID: id, Picture: result})
	}
	options.TrackInfo = func(info playback.VideoTracks) { d.send(PlaybackEvent{Kind: PlaybackTrackInfo, ID: id, Tracks: info}) }
	options.Subtitle = func(result playback.SubtitleResult) {
		d.send(PlaybackEvent{Kind: PlaybackSubtitle, ID: id, Subtitle: result})
	}
	options.StartTicks = offset
	options.Start = gate
	options.AsyncCleanup = cleanup
	d.cleanup.Add(1)
	options.CleanupDone = func() {
		defer d.cleanup.Done()
		d.send(PlaybackEvent{Kind: PlaybackCleanupDone, ID: id})
	}
	options.Controls = controls
	options.AcquireVideo = d.output.Acquire
	options.ReleaseVideo = d.output.Release
	options.ControlError = func(err error) { d.send(PlaybackEvent{Kind: PlaybackControlFailed, ID: id, Err: err}) }
	options.Paused = func(paused bool) { d.send(PlaybackEvent{Kind: PlaybackPaused, ID: id, Value: paused}) }
	options.Buffering = func(waiting bool) { d.send(PlaybackEvent{Kind: PlaybackBuffering, ID: id, Value: waiting}) }
	options.VideoStarted = func() { d.send(PlaybackEvent{Kind: PlaybackVideoStarted, ID: id}) }
	if prepared {
		options.Ready = func() { d.send(PlaybackEvent{Kind: PlaybackPrepared, ID: id}) }
	}
	go func() {
		defer close(finished)
		err := playback.Run(ctx, client, item, options, func(ticks int64) {
			// Progress is disposable. A full queue must not stall the decoder.
			select {
			case d.events <- PlaybackEvent{Kind: PlaybackPosition, ID: id, Ticks: ticks}:
			default:
			}
		})
		d.send(PlaybackEvent{Kind: PlaybackEnded, ID: id, Err: err})
	}()
	return playbackProcess{id: id, cancel: stop, done: finished, cleanup: cleanup}
}
