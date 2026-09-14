package browser

import (
	"context"
	"errors"
	"time"

	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/playback"
	"misterfin-crt/internal/videoout"
)

// Run owns the browser session and borrows input, output, and renderer. The loop
// serializes actions, worker results, playback events, and presentation. Handlers
// request redraws without presenting. Decoder callbacks may acquire and release
// output concurrently. The caller must keep renderer, output, and values
// referenced by player valid until Run returns.
//
// keys supplies semantic actions and immutable binding labels from any input
// source. Nil disables input. Closing keys while ctx is active returns an
// "input closed" error. Run does not close keys or cancel its caller's context.
//
// Cancellation and user exit stop pending work and wait for tracked decoders
// and detached server cleanup. The caller must cancel and join its input reader,
// then close output after Run returns.
func Run(ctx context.Context, config Config, player playback.Config, output videoout.Output, renderer Renderer, keys <-chan control.Event) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s := newBrowserSession(ctx, config, player, output, renderer)
	defer func() { cancel(); s.close() }()
	var frames <-chan struct{}
	if notifier, ok := output.(videoout.FrameNotifier); ok {
		var err error
		frames, err = notifier.FrameUpdates()
		if err != nil {
			return err
		}
	}
	s.authenticate()
	if err := s.draw(); err != nil {
		return err
	}
	for {
		redraw := true
		select {
		case <-ctx.Done():
			return nil
		case <-s.ticker.C:
			s.controller.Tick(time.Now())
		case <-frames:
			// The output has a newly published video frame. Draw it now using
			// the same scene and renderer as timer-driven animation updates.
		case key, ok := <-keys:
			if !ok {
				if ctx.Err() != nil {
					return nil
				}
				return errors.New("input closed")
			}
			s.controls = key.Labels
			redraw = s.handleKey(key.Action)
			if s.model.Quit {
				return nil
			}
		case event := <-s.driver.events:
			redraw = s.handlePlayback(event)
		case r := <-s.events:
			redraw = s.handleResult(r)
		}
		if redraw {
			if err := s.draw(); err != nil {
				return err
			}
		}
	}
}
