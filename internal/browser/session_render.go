package browser

import (
	"bytes"
	"time"
)

// draw owns frame pacing and the paused-overlay refresh check.
func (s *browserSession) draw() error {
	now := time.Now()
	scene := sceneFromModel(s.model, s.controller.Snapshot(now), s.status, s.selection.current, s.selection.err, now)
	interval := s.output.FrameInterval(scene.Video)
	if interval != s.frameInterval {
		s.frameInterval = interval
		s.ticker.Reset(interval)
	}
	frame := s.renderer.Render(s.geometry.Width, s.geometry.Height, scene)
	changed := !bytes.Equal(s.lastVideoOverlay, frame.Overlay)
	s.lastVideoOverlay = append(s.lastVideoOverlay[:0], frame.Overlay...)
	if err := s.output.Present(frame); err != nil {
		return err
	}
	if frame.Video && changed && scene.Playback.Paused {
		s.controller.Refresh()
	}
	return nil
}
