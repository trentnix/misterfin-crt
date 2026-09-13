package browser

import (
	"time"

	"misterfin-go/internal/jellyfin"
)

// Parent returns the view beneath the current screen. Scalars are copied. Items
// remain borrowed read-only, so a worker can search without moving the selection.
func (m *Model) Parent() (View, bool) {
	if len(m.Stack) < 2 {
		return View{}, false
	}
	return m.Stack[len(m.Stack)-2], true
}

// SelectAdjacent commits a completed neighbor search. The parent page and its
// selection change together with the detail item and title. The caller must
// reject stale requests before calling. No screen is added to the stack.
func (m *Model) SelectAdjacent(parent View, item jellyfin.Item) bool {
	if len(m.Stack) < 2 || m.Current().Detail == nil {
		return false
	}
	m.Stack[len(m.Stack)-2] = parent
	m.Current().Detail = &item
	m.Current().Title = item.Name
	m.Notice = ""
	return true
}

// ReturnToParent leaves the current screen without interpreting an input action.
// It invalidates listing results, clears media presentation state, and restores
// the parent's saved selection. At the root it does nothing and returns false.
func (m *Model) ReturnToParent() bool {
	if len(m.Stack) < 2 {
		return false
	}
	m.Generation++
	m.Stack = m.Stack[:len(m.Stack)-1]
	m.Current().Loading = false
	m.Notice = ""
	m.EndMusicQueue()
	m.photoControlsUntil = time.Time{}
	return true
}

// StartMusicQueue keeps the now-playing screen visible between decoder runs.
// The selected audio item remains the queue position until SelectAdjacent commits.
func (m *Model) StartMusicQueue() {
	if item := m.Current().Detail; item != nil && item.Type == "Audio" {
		m.musicQueue = true
	}
}

// EndMusicQueue returns an audio detail screen to normal browsing presentation.
// It does not stop a decoder or move the selection.
func (m *Model) EndMusicQueue() { m.musicQueue = false }

// MusicQueueActive reports whether now-playing should remain visible, including
// while the browser looks for the next track after the decoder has ended.
func (m *Model) MusicQueueActive() bool { return m.musicQueue }

// TogglePhotoControls shows or hides the photo navigation menu for three seconds.
// Photo controls have no decoder and never inherit a playback menu's seek pin.
func (m *Model) TogglePhotoControls(now time.Time) {
	if item := m.Current().Detail; item == nil || item.Type != "Photo" {
		return
	}
	if m.PhotoControlsVisible(now) {
		m.photoControlsUntil = time.Time{}
	} else {
		m.photoControlsUntil = now.Add(3 * time.Second)
	}
}

// PhotoControlsVisible reports the selected photo's navigation menu visibility.
func (m *Model) PhotoControlsVisible(now time.Time) bool {
	item := m.Current().Detail
	return item != nil && item.Type == "Photo" && now.Before(m.photoControlsUntil)
}
