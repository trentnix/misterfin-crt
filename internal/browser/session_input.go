package browser

import (
	"time"

	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/playback"
)

// dispatchKey routes each action to the active screen. The return value requests
// an immediate redraw. Quit remains owned by the event loop.
func (s *browserSession) dispatchKey(key control.Action) bool {
	if key == control.Quit {
		s.model.Quit = true
		return false
	}
	photo := s.model.Current().Detail != nil && s.model.Current().Detail.Type == "Photo"
	playing := s.controller.running || s.model.MusicQueueActive()
	repeated := key.IsRepeat()
	key = key.Base()
	if repeated {
		if key == control.About || s.about.Visible {
			return false
		}
		if playing && !s.controller.picker.visible && (menuDirection(key) || key == control.TrackPrevious || key == control.TrackNext) {
			return false
		}
		if key == control.Open || key == control.Back || key == control.Select || (photo && key == control.Up) {
			return false
		}
	}
	if s.about.Visible {
		return s.handleAboutKey(key)
	}
	if key == control.About {
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
		key = control.ToggleControls
	}
	if !playing {
		// Shoulder keys retain their existing page navigation outside playback.
		switch key {
		case control.TrackPrevious:
			key = control.Previous
		case control.TrackNext:
			key = control.Next
		}
	}
	if playing {
		if key == control.Back && s.remotePlayback.active && !s.controller.picker.visible {
			s.remoteRequests.cancelAll()
			s.remotePlayback.switching = false
			s.controller.stopByUser()
			return true
		}
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
		case control.Back:
			s.model.Quit = true
			return false
		case control.Retry, control.Open:
			if s.setup.retryLabel() != "" {
				s.authenticate()
			}
		}
		return true
	}
	if key == control.Back && s.remoteRequests.resolving {
		s.remoteRequests.cancelAll()
	}
	return s.handleBrowseKey(key)
}

func (s *browserSession) handleMusicKey(key control.Action) bool {
	now := time.Now()
	switch key {
	case control.Select:
		s.cycleMusicBackground()
	case control.Back:
		s.controller.Key(control.Back, now)
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
	case control.Open, control.ToggleControls, control.SeekBackward, control.SeekForward:
		s.controller.Key(key, now)
	case control.TrackPrevious, control.TrackNext:
		s.media.nextTrack = -1
		if key == control.TrackNext {
			s.media.nextTrack = 1
		}
		s.navigateMedia(s.media.nextTrack)
	}
	return true
}

func (s *browserSession) handlePendingMediaKey(key control.Action) bool {
	if key == control.Back {
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

func (s *browserSession) handlePhotoKey(key control.Action) {
	if key == control.Back {
		s.model.Notice = ""
	}
	switch key {
	case control.Up:
		s.model.TogglePhotoControls(time.Now())
	case control.Previous, control.Next, control.Down:
		direction := 1
		if key == control.Previous {
			direction = -1
		}
		s.navigateMedia(direction)
	}
}

func (s *browserSession) handleBrowseKey(key control.Action) bool {
	if key == control.Select && canShuffle(*s.model.Current()) {
		s.startShuffle()
		return true
	}
	if key == control.Retry && s.model.Current().Detail == nil && (s.model.Current().Location.Kind == "continue" || (s.model.Current().Item() != nil && s.model.Current().Item().ID == continueID)) {
		s.refreshHome()
		return true
	}
	depth := len(s.model.Stack)
	if key == control.Select && s.model.Notice == "" && resumableVideo(s.model.Current().Detail) {
		start := int64(0)
		s.startPlayback(&start, false)
		return false
	}
	if key == control.Open && s.model.Notice == "" && s.model.Current().Detail != nil && playback.Supported(*s.model.Current().Detail) {
		s.startPlayback(nil, false)
		return false
	}
	if key == control.Retry {
		if item := s.model.Current().Item(); item != nil {
			s.selection.loader.forget(*item)
		}
		s.selection.key = ""
	}
	before := s.model.Generation
	wasDetail := s.model.Current().Detail != nil
	req := s.model.Key(key)
	if key == control.Back && len(s.model.Stack) < depth && (len(s.model.Stack) == 1 || s.model.Current().Location.Kind == "continue") {
		s.refreshHome()
	}
	if s.model.Quit {
		return false
	}
	if s.model.Generation != before {
		s.requests.cancel()
	}
	s.load(req)
	if key == control.Open && !wasDetail && s.model.Current().Detail != nil && jellyfin.IsLive(*s.model.Current().Detail) {
		s.startPlayback(nil, false)
		return false
	}
	s.loadSelection()
	s.load(s.model.Prefetch())
	if key == control.Open && !wasDetail && s.model.Current().Detail != nil && s.model.Current().Detail.Type == "Audio" {
		s.startPlayback(nil, false)
	}

	return true
}

// menuDirection interprets directional navigation only while media is playing.
func menuDirection(key control.Action) bool {
	return key == control.Up || key == control.Down || key == control.Previous || key == control.Next
}
