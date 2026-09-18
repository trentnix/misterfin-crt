package browser

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"mistervision/internal/media"
	"mistervision/internal/playback"
)

// mediaNavigation owns adjacent-photo, music, and playlist navigation. It is separate
// from PlaybackController, which owns only the current item's decoder.
type mediaNavigation struct {
	cancel     context.CancelFunc
	generation int
	pending    bool
	paused     bool // Explicit pause intent for the next item. New navigation starts playing.
	nextTrack  int
	queued     *mediaSelection
}

// mediaSelection holds a resolved item while the current decoder stops.
type mediaSelection struct {
	parent View
	item   media.Item
}

// navigateMedia looks for the previous (-1) or next (1) photo, track, or playlist entry.
// Only one neighbor request runs at a time. The displayed item remains selected
// until a matching result arrives and any current decoder has stopped.
func (s *browserSession) navigateMedia(direction int) {
	if s.playbackQueue.active {
		s.moveQueue(direction, false)
		return
	}
	if s.shuffle.library != "" {
		s.navigateShuffle(direction)
		return
	}
	parent, ok := s.model.Parent()
	if !ok || s.model.Current().Detail == nil || s.media.pending {
		return
	}
	s.media.cancel()
	s.media.generation++
	generation := s.media.generation
	kind := s.model.Current().Detail.Type
	if parent.Location.Kind == "playlist" && kind != "Photo" {
		kind = "playlist"
	}
	rows := s.model.Rows
	work, stop := context.WithCancel(s.ctx)
	s.media.cancel = stop
	s.media.pending = true
	s.media.nextTrack = direction
	s.media.paused = false
	client := s.client
	go func() {
		parent, item, err := adjacentMedia(work, client, parent, kind, direction, rows)
		s.send(work, neighborResult{generation: generation, parent: parent, item: item, err: err})
	}()
}

func (s *browserSession) startPlayback(startTicks *int64, paused bool) {
	if !s.playbackQueue.active {
		s.remoteRequests.cancelAll()
	}
	if !s.message.preserveOnPlayback {
		s.message = browserMessage{}
	}
	s.music.levels = playback.AudioLevels{}
	selected := *s.model.Current().Detail
	if selected.Type != "Audio" {
		s.output.Clear()
		s.selection.cancel()
		s.selection.generation++
	}
	s.model.EndMusicQueue()
	if selected.Type == "Audio" {
		s.model.StartMusicQueue()
	}
	s.controller.Start(selected, startTicks, paused, time.Now())
	s.media.paused = false
	s.publishLocalQueue()
	s.media.nextTrack = 0
	s.model.Notice = ""
}

// handlePlayback applies decoder feedback, then handles item completion.
// Seek handoffs stay inside the controller and do not advance queues.
func (s *browserSession) handlePlayback(event PlaybackEvent) bool {
	if event.Kind == PlaybackCleanupDone {
		s.controller.Handle(event, time.Now())
		// A stopped item can become visible before its resume save completes.
		// Refresh it after cleanup, without disturbing a newer playback session.
		if !s.controller.running && event.ID == s.controller.active.id && s.controller.item.Type != "Audio" && !media.IsLive(s.controller.item) {
			s.refreshHome()
			if detail := s.model.Current().Detail; detail != nil && detail.ID == s.controller.item.ID {
				s.selection.key = ""
				s.loadSelection()
			}
		}
		return false
	}
	if event.Kind == PlaybackLevels {
		if s.controller.running && event.ID == s.controller.active.id {
			s.music.levels = event.Levels
			s.music.levelTime = time.Now()
		}
		return false
	}
	// Progress reporting is independent of decoder completion. Normalize it
	// once before the controller and either queue decide how to advance.
	completionErr := event.Err
	if event.Kind == PlaybackEnded && errors.Is(event.Err, playback.ErrProgress) {
		event.Err = nil
	}
	progressSeen, subtitleLoading := s.controller.state.ProgressSeen, s.controller.subtitleLoading
	ended := s.controller.Handle(event, time.Now())
	// Decoder feedback and server reports precede application on the UI loop.
	// Record accepted transitions so diagnostics distinguish preparation from
	// controls that are ready for input. Ignore stale decoder generations.
	if s.controller.running && event.ID == s.controller.active.id {
		if event.Kind == PlaybackPosition && !progressSeen && s.controller.state.ProgressSeen {
			s.config.Diagnostics.Record("browser.playback-ready", slog.Int("generation", event.ID), slog.Int64("position_ticks", event.Ticks))
		}
		if event.Kind == PlaybackSubtitle && subtitleLoading && !s.controller.subtitleLoading && event.Subtitle.Err == nil {
			s.config.Diagnostics.Record("browser.subtitle", slog.Int("generation", event.ID), slog.Int("index", s.controller.tracks.Selection.SubtitleIndex))
		}
	}
	if s.controller.notice != "" {
		s.model.Notice = s.controller.notice
	}
	if !s.controller.running {
		s.output.Clear()
	}
	if !ended {
		return false
	}
	s.model.Notice = ""
	if completionErr != nil {
		s.showPlaybackError(completionErr)
	}
	if s.controller.item.Type != "Audio" && !media.IsLive(s.controller.item) {
		s.refreshHome()
	}
	if s.queueEnded(event) {
		return true
	}
	if (s.model.MusicQueueActive() || s.playlistPlayback()) && !s.controller.stoppedByUser && event.Err == nil && s.media.queued != nil {
		queued := s.media.queued
		s.media.queued = nil
		if !s.model.SelectAdjacent(queued.parent, queued.item) {
			return false
		}
		s.selection.key = ""
		s.loadSelection()
		zero := int64(0)
		s.startPlayback(&zero, s.media.paused)
	} else if (s.model.MusicQueueActive() || s.playlistPlayback()) && !s.controller.stoppedByUser && event.Err == nil {
		direction := s.media.nextTrack
		if direction == 0 {
			direction = 1
		}
		s.navigateMedia(direction)
	} else {
		s.media.cancel()
		s.media.generation++
		s.media.pending = false
		s.media.queued = nil
		s.media.nextTrack = 0
		wasAudio := s.model.MusicQueueActive()
		s.shuffle = shuffleQueue{}
		s.model.EndMusicQueue()
		if ((wasAudio || s.playlistPlayback()) && s.controller.stoppedByUser) || (s.model.Current().Detail != nil && media.IsLive(*s.model.Current().Detail)) {
			s.model.ReturnToParent()
		}
		s.selection.key = ""
		s.loadSelection()
	}

	return true
}

func (s *browserSession) handleNeighbor(r neighborResult) bool {
	if r.generation != s.media.generation {
		return false
	}
	s.media.pending = false
	direction := s.media.nextTrack
	s.media.nextTrack = 0
	if s.controller.running && (s.model.MusicQueueActive() || s.playlistPlayback()) {
		if r.item != nil && r.err == nil {
			s.media.queued = &mediaSelection{parent: r.parent, item: *r.item}
			s.controller.StopForTrackChange()
		}
		if r.err != nil {
			s.model.Notice = neighborFailure(direction, "track")
		}
		return false
	}
	if r.err != nil {
		s.model.Notice = neighborFailure(direction, "item")
		s.model.EndMusicQueue()
	} else if r.item != nil {
		if !s.model.SelectAdjacent(r.parent, *r.item) {
			return false
		}
		s.selection.key = ""
		s.loadSelection()
		if r.item.Type == "Audio" || (r.parent.Location.Kind == "playlist" && playback.Supported(*r.item)) {
			zero := int64(0)
			s.startPlayback(&zero, s.media.paused)
		}
	} else if s.model.MusicQueueActive() || s.playlistPlayback() {
		s.model.ReturnToParent()
		s.loadSelection()
	}

	return true
}

// playlistPlayback identifies an ordered local audio/video list. It also stays
// true during the asynchronous handoff after one item ends.
func (s *browserSession) playlistPlayback() bool {
	parent, ok := s.model.Parent()
	item := s.model.Current().Detail
	return ok && parent.Location.Kind == "playlist" && item != nil && playback.Supported(*item) && !media.IsLive(*item)
}

// localPlaybackPending identifies a local track transition, excluding photos
// and canceled playback. Its pause intent remains valid between decoders.
func (s *browserSession) localPlaybackPending() bool {
	return !s.controller.stoppedByUser && (s.media.pending || s.media.queued != nil) &&
		(s.model.MusicQueueActive() || s.playlistPlayback())
}

// wantsPause reads intent from the owner of the next audible playback.
func (s *browserSession) wantsPause() bool {
	if s.playbackQueue.switching {
		return s.playbackQueue.paused
	}
	if s.localPlaybackPending() {
		return s.media.paused
	}
	return s.controller.wantsPause()
}

// setPaused routes local and remote intent to playback or its pending successor.
// While lookup runs, the current decoder also pauses so it does not keep playing
// after the user presses Pause. A stopping decoder receives no further commands.
func (s *browserSession) setPaused(paused bool) bool {
	switch {
	case s.playbackQueue.switching:
		s.playbackQueue.paused = paused
	case s.localPlaybackPending():
		s.media.paused = paused
		if s.media.queued == nil && s.controller.running {
			s.controller.SetPaused(paused)
		}
	case s.controller.running && !s.controller.stoppedByUser:
		s.controller.SetPaused(paused)
	default:
		return false
	}
	return true
}
