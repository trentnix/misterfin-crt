package browser

import (
	"strings"

	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/sound"
)

// navigationState captures only user-visible browsing changes. Artwork arrivals
// and redraws cannot produce sounds. Absolute selection survives page prefetch.
type navigationState struct {
	depth, selected   int
	location          jellyfin.Location
	detail, notice    string
	list, exit, media bool
}

// navigationState takes a small value snapshot before or after an input action.
func (s *browserSession) navigationState() navigationState {
	v := s.model.Current()
	n := navigationState{depth: len(s.model.Stack), selected: v.Start + v.Selected, location: v.Location,
		notice: s.model.Notice, list: s.model.ListMode, exit: s.model.ExitConfirm,
		media: s.controller.running || s.model.MusicQueueActive() || s.media.pending}
	if v.Loading {
		n.selected = v.Target
	}
	if v.Detail != nil {
		n.detail = v.Detail.ID
	}
	return n
}

// handleKey adds semantic audio feedback around input dispatch. Browsing actions
// sound only when selection or screen state changes. Playback controls stay silent.
func (s *browserSession) handleKey(key string) bool {
	if s.feedback == nil {
		return s.dispatchKey(key)
	}
	before := s.navigationState()
	redraw := s.dispatchKey(key)
	after := s.navigationState()
	if before == after || before.media || after.media || s.model.Quit {
		return redraw
	}
	action := strings.TrimSuffix(key, "-repeat")
	switch action {
	case "open", "back", "select":
		s.feedback.Play(sound.Confirm)
	case "up", "down", "previous", "next", "track-previous", "track-next":
		if before.depth == after.depth && before.location == after.location && before.selected != after.selected {
			s.feedback.Play(sound.Navigate)
		}
	}
	return redraw
}
