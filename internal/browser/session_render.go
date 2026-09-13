package browser

import (
	"bytes"
	"time"
)

// draw owns frame pacing and the paused-overlay refresh check.
func (s *browserSession) draw() error {
	now := time.Now()
	// Browser motion follows the C client's 60 Hz timeline. Video owns its
	// decoding cadence and only needs the existing 30 Hz overlay updates.
	interval := time.Second / 60
	if s.controller.running && s.model.PlayingVideo {
		interval = time.Second / 30
	}
	if interval != s.frameInterval {
		s.frameInterval = interval
		s.ticker.Reset(interval)
	}
	scene := sceneFromModel(s.model, s.status, s.artwork.current, s.artwork.err, now)
	scene.Video = s.controller.running && s.model.PlayingVideo
	scene.Playback = s.controller.Snapshot(now)
	frame := s.renderer.Render(s.geometry.Width, s.geometry.Height, scene)
	changed := !bytes.Equal(s.lastVideoOverlay, frame.Overlay)
	s.lastVideoOverlay = append(s.lastVideoOverlay[:0], frame.Overlay...)
	if err := s.output.Present(frame); err != nil {
		return err
	}
	if frame.Video && changed && s.model.Paused {
		s.controller.Refresh()
	}
	return nil
}
