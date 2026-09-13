package browser

import (
	"context"
	"time"

	"misterfin-go/internal/jellyfin"
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
	if len(s.model.Stack) < 2 || s.model.Current().Detail == nil || s.media.pending {
		return
	}
	s.media.cancel()
	s.media.generation++
	generation := s.media.generation
	parent := s.model.Stack[len(s.model.Stack)-2]
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
	selected := *s.model.Current().Detail
	if selected.Type != "Audio" {
		s.output.Clear()
		s.artwork.cancel()
		s.artwork.generation++
	}
	s.controller.Start(selected, startTicks, paused, time.Now())
	s.media.nextTrack = 0
	s.model.Notice = ""
}

// handlePlayback applies decoder feedback, then handles item completion.
// Seek handoffs stay inside the controller and do not advance the music queue.
func (s *browserSession) handlePlayback(event PlaybackEvent) bool {
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
	if s.model.PlayingAudio && !s.controller.stoppedByUser && event.Err == nil && s.media.queued != nil {
		s.model.Stack[len(s.model.Stack)-2] = s.media.queued.parent
		s.model.Current().Detail = s.media.queued.item
		s.model.Current().Title = s.media.queued.item.Name
		s.media.queued = nil
		s.artwork.key = ""
		s.loadArt()
		s.startPlayback(nil, false)
	} else if s.model.PlayingAudio && !s.controller.stoppedByUser && event.Err == nil {
		direction := s.media.nextTrack
		if direction == 0 {
			direction = 1
		}
		s.navigateMedia(direction)
	} else {
		wasAudio := s.model.PlayingAudio
		s.model.PlayingAudio = false
		s.model.HideControls()
		if (wasAudio && s.controller.stoppedByUser) || (s.model.Current().Detail != nil && jellyfin.IsLive(*s.model.Current().Detail)) {
			s.load(s.model.Key("back"))
		}
		s.artwork.key = ""
		s.loadArt()
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
	if s.controller.running && s.model.PlayingAudio {
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
		s.model.PlayingAudio = false
	} else if r.item != nil {
		s.model.Stack[len(s.model.Stack)-2] = r.parent
		s.model.Current().Detail = r.item
		s.model.Current().Title = r.item.Name
		s.model.Notice = ""
		s.artwork.key = ""
		s.loadArt()
		if r.item.Type == "Audio" {
			s.startPlayback(nil, false)
		}
	} else if s.model.PlayingAudio {
		s.model.PlayingAudio = false
		s.load(s.model.Key("back"))
		s.loadArt()
	}

	return true
}
