package browser

import (
	"strings"
	"time"

	"misterfin-go/internal/jellyfin"
	"misterfin-go/internal/playback"
)

// handleKey routes each action to the active screen. The return value requests
// an immediate redraw. Quit remains owned by the event loop.
func (s *browserSession) handleKey(key string) bool {
	if key == "quit" {
		s.model.Quit = true
		return false
	}
	photo := s.model.Current().Detail != nil && s.model.Current().Detail.Type == "Photo"
	if strings.HasSuffix(key, "-repeat") {
		if key == "up-repeat" && (s.controller.running || photo) {
			return false
		}
		key = strings.TrimSuffix(key, "-repeat")
	}
	if s.controller.running {
		if s.model.PlayingAudio {
			return s.handleMusicKey(key)
		}
		s.controller.Key(key, time.Now())
		if !s.controller.running {
			s.output.Clear()
		}
		return true
	}
	if s.media.pending {
		return s.handlePendingMediaKey(key)
	}
	if photo {
		s.handlePhotoKey(key)
	}
	if s.status != "" {
		switch key {
		case "back":
			s.model.Quit = true
			return false
		case "retry", "open":
			s.authenticate()
		}
		return true
	}
	return s.handleBrowseKey(key)
}

func (s *browserSession) handleMusicKey(key string) bool {
	now := time.Now()
	switch key {
	case "back":
		s.controller.Key("back", now)
		s.media.cancel()
		s.media.generation++
		s.media.pending = false
		s.media.queued = nil
		s.media.nextTrack = 0
	case "open", "up":
		s.controller.Key(key, now)
	case "previous", "next":
		s.media.nextTrack = -1
		if key == "next" {
			s.media.nextTrack = 1
		}
		s.navigateMedia(s.media.nextTrack)
	}
	return true
}

func (s *browserSession) handlePendingMediaKey(key string) bool {
	if key == "back" {
		s.media.cancel()
		s.media.generation++
		s.media.pending = false
		s.model.PlayingAudio = false
		s.model.Notice = ""
		s.load(s.model.Key("back"))
		s.loadArt()
	}
	return false
}

func (s *browserSession) handlePhotoKey(key string) {
	if key == "back" {
		s.model.Notice = ""
		s.model.HideControls()
	}
	switch key {
	case "up":
		s.model.ToggleControls(time.Now())
	case "previous", "next", "down":
		direction := 1
		if key == "previous" {
			direction = -1
		}
		s.navigateMedia(direction)
	}
}

func (s *browserSession) handleBrowseKey(key string) bool {
	if key == "select" && s.model.Notice == "" && resumableVideo(s.model.Current().Detail) {
		start := int64(0)
		s.startPlayback(&start, false)
		return false
	}
	if key == "open" && s.model.Notice == "" && s.model.Current().Detail != nil && playback.Supported(*s.model.Current().Detail) {
		s.startPlayback(nil, false)
		return false
	}
	if key == "retry" {
		if item := s.model.Current().Item(); item != nil {
			s.artwork.loader.cache.forget(*item)
		}
		s.artwork.key = ""
	}
	before := s.model.Generation
	wasDetail := s.model.Current().Detail != nil
	req := s.model.Key(key)
	if key == "open" && s.model.Current().Detail != nil && s.model.Current().Detail.Type == "Photo" {
		s.model.HideControls()
	}
	if s.model.Quit {
		return false
	}
	if s.model.Generation != before {
		s.requests.cancel()
	}
	s.load(req)
	if key == "open" && !wasDetail && s.model.Current().Detail != nil && jellyfin.IsLive(*s.model.Current().Detail) {
		s.startPlayback(nil, false)
		return false
	}
	s.loadArt()
	if key == "open" && !wasDetail && s.model.Current().Detail != nil && s.model.Current().Detail.Type == "Audio" {
		s.startPlayback(nil, false)
	}

	return true
}
