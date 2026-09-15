package browser

import (
	"strings"
	"time"

	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/playback"
)

// dispatchKey routes each action to the active screen. The return value requests
// an immediate redraw. Quit remains owned by the event loop.
func (s *browserSession) dispatchKey(key string) bool {
	if key == "quit" {
		s.model.Quit = true
		return false
	}
	photo := s.model.Current().Detail != nil && s.model.Current().Detail.Type == "Photo"
	playing := s.controller.running || s.model.MusicQueueActive()
	repeated := strings.HasSuffix(key, "-repeat")
	key = strings.TrimSuffix(key, "-repeat")
	if repeated {
		if key == "about" || s.about.Visible {
			return false
		}
		if playing && !s.controller.picker.visible && (menuDirection(key) || key == "track-previous" || key == "track-next") {
			return false
		}
		if key == "open" || key == "back" || key == "select" || (photo && key == "up") {
			return false
		}
	}
	if s.about.Visible {
		return s.handleAboutKey(key)
	}
	if key == "about" {
		if playing || photo || s.media.pending {
			return false
		}
		s.about.Visible = true
		if !s.about.Checked {
			s.checkUpdate()
		}
		return true
	}
	if playing && !s.controller.picker.visible && menuDirection(key) {
		key = "controls"
	}
	if !playing {
		// Shoulder keys retain their existing page navigation outside playback.
		switch key {
		case "track-previous":
			key = "previous"
		case "track-next":
			key = "next"
		}
	}
	if playing {
		if s.model.MusicQueueActive() {
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
	if s.setup.Kind != SetupHidden {
		switch key {
		case "back":
			s.model.Quit = true
			return false
		case "retry", "open":
			if s.setup.retryLabel() != "" {
				s.authenticate()
			}
		}
		return true
	}
	return s.handleBrowseKey(key)
}

func (s *browserSession) handleMusicKey(key string) bool {
	now := time.Now()
	switch key {
	case "select":
		s.cycleMusicBackground()
	case "back":
		s.controller.Key("back", now)
		s.media.cancel()
		s.media.generation++
		s.media.pending = false
		s.media.queued = nil
		s.media.nextTrack = 0
		if !s.controller.running {
			s.shuffle = shuffleQueue{}
			s.model.ReturnToParent()
			s.loadSelection()
		}
	case "open", "controls", "seek-backward", "seek-forward":
		s.controller.Key(key, now)
	case "track-previous", "track-next":
		s.media.nextTrack = -1
		if key == "track-next" {
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
		if s.shuffle.library == "" || s.model.MusicQueueActive() {
			s.model.ReturnToParent()
		}
		s.shuffle = shuffleQueue{}
		s.model.Notice = ""
		s.loadSelection()
	}
	return false
}

func (s *browserSession) handlePhotoKey(key string) {
	if key == "back" {
		s.model.Notice = ""
	}
	switch key {
	case "up":
		s.model.TogglePhotoControls(time.Now())
	case "previous", "next", "down":
		direction := 1
		if key == "previous" {
			direction = -1
		}
		s.navigateMedia(direction)
	}
}

func (s *browserSession) handleBrowseKey(key string) bool {
	if key == "select" && canShuffle(*s.model.Current()) {
		s.startShuffle()
		return true
	}
	if key == "retry" && s.model.Current().Detail == nil && (s.model.Current().Location.Kind == "continue" || (s.model.Current().Item() != nil && s.model.Current().Item().ID == continueID)) {
		s.refreshHome()
		return true
	}
	depth := len(s.model.Stack)
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
			s.selection.loader.forget(*item)
		}
		s.selection.key = ""
	}
	before := s.model.Generation
	wasDetail := s.model.Current().Detail != nil
	req := s.model.Key(key)
	if key == "back" && len(s.model.Stack) < depth && (len(s.model.Stack) == 1 || s.model.Current().Location.Kind == "continue") {
		s.refreshHome()
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
	s.loadSelection()
	s.load(s.model.Prefetch())
	if key == "open" && !wasDetail && s.model.Current().Detail != nil && s.model.Current().Detail.Type == "Audio" {
		s.startPlayback(nil, false)
	}

	return true
}

// menuDirection interprets directional navigation only while media is playing.
func menuDirection(key string) bool {
	return key == "up" || key == "down" || key == "previous" || key == "next"
}
