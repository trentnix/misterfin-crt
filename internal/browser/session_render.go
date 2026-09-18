package browser

import (
	"bytes"
	"time"

	"mistervision/internal/musicviz"
	"mistervision/internal/rendering"
)

// draw owns frame pacing and the paused-overlay refresh check.
func (s *browserSession) draw() error {
	now := time.Now()
	scene := sceneFromModel(s.model, s.controller.Snapshot(now), s.setup, s.selection.current, s.selection.err, now)
	if item := scene.Content.Item(); scene.Root && item != nil && item.ID == continueID {
		scene.LibraryLoading = !s.home.loaded
	}
	// Start the notice timer when browsing can actually display it, including
	// after a long Quick Connect sign-in. Consume it so navigation cannot repeat it.
	if len(s.startupNotices) > 0 && !now.Before(s.message.Until) && scene.Setup.Kind == rendering.SetupHidden && !s.about.Visible && scene.Content.Detail == nil && !scene.Content.Loading && scene.Content.Error == "" {
		s.message = browserMessage{MessagePresentation: rendering.MessagePresentation{Header: "Settings", Text: s.startupNotices[0], Until: now.Add(4 * time.Second)}}
		s.startupNotices = s.startupNotices[1:]
	}
	scene.Title = s.config.Title
	scene.Background = s.config.Background
	scene.Message = s.message.MessagePresentation
	scene.About = s.about
	scene.Controls = s.controls
	scene.Music, scene.MusicIndex = s.music.library, s.music.index
	scene.Shuffle = s.shuffle.library != "" || (s.playbackQueue.active && s.playbackQueue.queue.Shuffled())
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
	if err := s.output.Present(frame); err != nil {
		return err
	}
	if !frame.Video || !scene.Playback.Paused {
		s.lastVideoOverlay = s.lastVideoOverlay[:0]
	} else if !bytes.Equal(s.lastVideoOverlay, frame.Overlay) && s.controller.Refresh() {
		// A busy decoder must retry on the next frame. Remember the overlay
		// only after both presentation and the redraw request succeed.
		s.lastVideoOverlay = append(s.lastVideoOverlay[:0], frame.Overlay...)
	}
	return nil
}
