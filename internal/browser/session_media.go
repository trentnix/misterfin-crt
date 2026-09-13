package browser

import (
	"context"
	"time"

	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/playback"
)

// mediaNavigation owns adjacent-photo and music-queue work. It is separate
// from PlaybackController, which owns only the current item's decoder.
type mediaNavigation struct {
	cancel     context.CancelFunc
	generation int
	pending    bool
	nextTrack  int
	queued     *result
}

// navigateMedia looks for the previous (-1) or next (1) photo or music track.
// Only one neighbor request runs at a time. The displayed item remains selected
// until a matching result arrives and any current decoder has stopped.
func (s *browserSession) navigateMedia(direction int) {
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
	rows := s.model.Rows
	work, stop := context.WithCancel(s.ctx)
	s.media.cancel = stop
	s.media.pending = true
	client := s.client
	go func() {
		parent, item, err := adjacentMedia(work, client, parent, kind, direction, rows)
		s.send(work, result{kind: neighborResult, mediaGeneration: generation, parent: parent, item: item, err: err})
	}()
}

func (s *browserSession) startPlayback(startTicks *int64, paused bool) {
	s.music.levels = playback.AudioLevels{}
	selected := *s.model.Current().Detail
	if selected.Type != "Audio" {
		s.output.Clear()
		s.selection.cancel()
		s.selection.generation++
	}
	if selected.Type == "Audio" {
		s.model.StartMusicQueue()
	}
	s.controller.Start(selected, startTicks, paused, time.Now())
	s.media.nextTrack = 0
	s.model.Notice = ""
}

// handlePlayback applies decoder feedback, then handles item completion.
// Seek handoffs stay inside the controller and do not advance the music queue.
func (s *browserSession) handlePlayback(event PlaybackEvent) bool {
	if event.Kind == PlaybackCleanupDone {
		s.controller.Handle(event, time.Now())
		// A stopped item can become visible before its resume save completes.
		// Refresh it after cleanup, without disturbing a newer playback session.
		if !s.controller.running && event.ID == s.controller.active.id && s.controller.item.Type != "Audio" && !jellyfin.IsLive(s.controller.item) {
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
	ended := s.controller.Handle(event, time.Now())
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
	if s.controller.item.Type != "Audio" && !jellyfin.IsLive(s.controller.item) {
		s.refreshHome()
	}
	if s.model.MusicQueueActive() && !s.controller.stoppedByUser && event.Err == nil && s.media.queued != nil {
		queued := s.media.queued
		s.media.queued = nil
		if !s.model.SelectAdjacent(queued.parent, *queued.item) {
			return false
		}
		s.selection.key = ""
		s.loadSelection()
		s.startPlayback(nil, false)
	} else if s.model.MusicQueueActive() && !s.controller.stoppedByUser && event.Err == nil {
		direction := s.media.nextTrack
		if direction == 0 {
			direction = 1
		}
		s.navigateMedia(direction)
	} else {
		wasAudio := s.model.MusicQueueActive()
		s.shuffle = shuffleQueue{}
		s.model.EndMusicQueue()
		if (wasAudio && s.controller.stoppedByUser) || (s.model.Current().Detail != nil && jellyfin.IsLive(*s.model.Current().Detail)) {
			s.model.ReturnToParent()
		}
		s.selection.key = ""
		s.loadSelection()
		if event.Err != nil {
			s.model.Notice = event.Err.Error() + "  A:back"
		}
	}

	return true
}

func (s *browserSession) handleNeighbor(r result) bool {
	if r.mediaGeneration != s.media.generation {
		return false
	}
	s.media.pending = false
	s.media.nextTrack = 0
	if s.controller.running && s.model.MusicQueueActive() {
		if r.item != nil && r.err == nil {
			s.media.queued = &r
			s.controller.StopForTrackChange()
		}
		if r.err != nil {
			s.model.Notice = "Could not load adjacent track"
		}
		return false
	}
	if r.err != nil {
		s.model.Notice = "Could not load adjacent item. A:back"
		s.model.EndMusicQueue()
	} else if r.item != nil {
		if !s.model.SelectAdjacent(r.parent, *r.item) {
			return false
		}
		s.selection.key = ""
		s.loadSelection()
		if r.item.Type == "Audio" {
			s.startPlayback(nil, false)
		}
	} else if s.model.MusicQueueActive() {
		s.model.ReturnToParent()
		s.loadSelection()
	}

	return true
}
