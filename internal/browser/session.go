package browser

import (
	"context"
	"time"

	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/platform"
	"misterfin-crt/internal/playback"
	"misterfin-crt/internal/videoout"
)

// browserSession owns one browser run. Only the event loop mutates its state.
// Workers capture their inputs and return results through channels.
type browserSession struct {
	ctx              context.Context
	config           Config
	model            *Model
	client           *jellyfin.Client
	status           string
	requests         requestState
	home             homeState
	selection        selectionState
	media            mediaNavigation
	shuffle          shuffleQueue
	music            musicPresentation
	events           chan workerResult
	controller       *PlaybackController
	driver           playbackDriver
	output           videoout.Output
	renderer         Renderer
	geometry         platform.Geometry
	ticker           *time.Ticker
	frameInterval    time.Duration
	lastVideoOverlay []byte
	controls         control.Labels
}

// newBrowserSession wires state, decoding, and frame pacing without starting
// network requests. The caller must cancel ctx before calling close. The caller
// retains ownership of output and renderer, which must not be used concurrently.
func newBrowserSession(ctx context.Context, config Config, player playback.Config, output videoout.Output, renderer Renderer) *browserSession {
	s := &browserSession{
		ctx: ctx, config: config,
		model: New(), output: output, renderer: renderer, geometry: output.Geometry(),
		events: make(chan workerResult, 16), frameInterval: time.Second / 60,
		requests:  requestState{cancel: func() {}},
		selection: selectionState{cancel: func() {}},
		media:     mediaNavigation{cancel: func() {}},
	}
	s.driver = playbackDriver{ctx: ctx, config: player, output: output, events: make(chan PlaybackEvent, 16)}
	s.controller = newPlaybackController(func(item jellyfin.Item, offset *int64, gate <-chan struct{}, prepared bool, controls chan playback.Control, tracks playback.TrackOptions) playbackProcess {
		return s.driver.launch(s.client, item, offset, gate, prepared, controls, tracks)
	})
	s.model.Rows = visibleRows(s.geometry.Width, s.geometry.Height)
	s.loadMusicConfig()
	s.ticker = time.NewTicker(s.frameInterval)
	return s
}

// close runs after the application context is canceled, so decoder callbacks
// cannot block shutdown while the event loop is no longer receiving results.
func (s *browserSession) close() {
	s.ticker.Stop()
	s.requests.cancel()
	if s.home.cancel != nil {
		s.home.cancel()
	}
	s.selection.cancel()
	s.media.cancel()
	s.controller.Close()
	s.driver.cleanup.Wait()
	s.output.Clear()
}
