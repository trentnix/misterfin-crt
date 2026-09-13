package browser

import (
	"context"
	"errors"
	"time"

	"misterfin-go/internal/input"
	"misterfin-go/internal/input/evdev"
	"misterfin-go/internal/playback"
	"misterfin-go/internal/videoout"
)

// Run owns input and session lifetime. The loop serializes actions, worker
// results, and playback events. Handlers request redraws without presenting.
//
// Run borrows output and renderer. Rendering and presentation are serial.
// Decoder callbacks may acquire and release output concurrently. The caller
// must close output after Run returns. Cancellation and user exit stop pending work and
// wait for tracked decoders and the input reader. Final server reporting may
// still be running when a seek handoff enabled asynchronous cleanup.
func Run(ctx context.Context, configPath, stateDir string, player playback.Options, output videoout.Output, renderer Renderer, bindings evdev.Config) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	keys, done, err := input.Read(ctx, player.Headless, bindings)
	if err != nil {
		output.Clear()
		return err
	}
	s := newBrowserSession(ctx, configPath, stateDir, player, output, renderer)
	defer func() { cancel(); s.close(); <-done }()
	var frames <-chan struct{}
	if notifier, ok := output.(videoout.FrameNotifier); ok {
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
