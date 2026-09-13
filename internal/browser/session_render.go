package browser

import (
	"bytes"
	"misterfin-crt/internal/musicviz"
	"time"
)

// draw owns frame pacing and the paused-overlay refresh check.
func (s *browserSession) draw() error {
	now := time.Now()
	scene := sceneFromModel(s.model, s.controller.Snapshot(now), s.status, s.selection.current, s.selection.err, now)
	if item := scene.View.Item(); scene.Root && item != nil && item.ID == continueID {
		scene.LibraryLoading = !s.home.loaded
	}
	scene.Controls = s.controls
	scene.Music, scene.MusicIndex = s.music.library, s.music.index
	scene.Shuffle = s.shuffle.library != ""
	scene.MusicMessage = s.music.error
	if s.music.library != nil && !s.music.library.Ready(s.music.index) && s.music.loading {
		scene.MusicMessage = "Loading background..."
	}
	scene.MusicLabel = now.Before(s.music.labelUntil)
	levels := [2]float64(s.music.levels)
	if now.Sub(s.music.levelTime) > 250*time.Millisecond || !s.controller.running {
		levels = [2]float64{}
	}
	scene.MusicFrame = musicviz.Frame{Now: now, Levels: levels, Paused: scene.Playback.Paused, Artwork: scene.Artwork.Primary}
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
