package browser

import (
	"time"

	"misterfin-go/internal/jellyfin"
)

// playbackState belongs exclusively to PlaybackController. Decoder events and
// controller actions update it on the browser loop. Renderers receive snapshots.
type playbackState struct {
	PlayingVideo bool
	Paused       bool

	// SeekTarget is an absolute Jellyfin position in 100-nanosecond ticks.
	// A nil target means no seek is queued. The second press reveals the preview.
	SeekTarget      *int64
	SeekPresses     int
	SwitchingTracks bool // A track handoff uses loading wording rather than seeking.
	SeekInFlight    bool // Preparation or decoder handoff is in progress.
	SeekDeadline    time.Time

	PositionTicks  int64
	ProgressSeen   bool      // The current decoder has reported its first position.
	VideoStarted   bool      // The current decoder has presented a frame, even without a position.
	LastAdvance    time.Time // Used to infer buffering when no explicit status exists.
	Buffering      bool
	BufferingKnown bool // The decoder has supplied an explicit buffering status.

	ControlsUntil time.Time // Menu timeout outside a seek.
	SeekControls  bool      // Keep the open menu through seek preparation and startup.
}

// RevealControls extends the three-second menu window and reports whether it
// was hidden. The duration follows bb31e83 and src/pause_ui.c.
func (m *playbackState) RevealControls(now time.Time) bool {
	hidden := !m.ControlsVisible(now)
	m.ControlsUntil = now.Add(3 * time.Second)
	return hidden
}

// ControlsVisible reports whether the menu timer is active or a seek pins it open.
func (m *playbackState) ControlsVisible(now time.Time) bool {
	return m.SeekControls || now.Before(m.ControlsUntil)
}

// HideControls dismisses the menu and clears any seek pin without canceling the seek.
func (m *playbackState) HideControls() {
	m.ControlsUntil = time.Time{}
	m.SeekControls = false
}

// seekVideo accumulates seek actions against the pending destination. It only
// updates UI intent. The controller starts the request after the deadline.
func (m *playbackState) seekVideo(item *jellyfin.Item, key string, now time.Time) {
	if !m.PlayingVideo || !m.ProgressSeen || item == nil || jellyfin.IsLive(*item) {
		return
	}
	target := m.PositionTicks
	if m.SeekTarget == nil {
		m.SeekPresses = 0
		m.SeekControls = m.ControlsVisible(now)
	}
	if m.SeekTarget != nil {
		target = *m.SeekTarget
	}
	step := int64(30 * 10000000)
	if key == "seek-backward" {
		step = -step
	}
	target = max(int64(0), target+step)
	if item.RunTimeTicks > 0 {
		target = min(target, max(int64(0), item.RunTimeTicks-10000000))
	}
	m.SeekTarget = &target
	m.SeekDeadline = now.Add(500 * time.Millisecond)
	m.SeekPresses++
}

// videoWaitLabel gives seeking priority over pause, then uses decoder feedback
// or a three-second position stall to distinguish loading from buffering.
func (m *playbackState) videoWaitLabel(now time.Time) string {
	if m.PlayingVideo && m.SeekInFlight {
		if m.SwitchingTracks {
			return "Loading..."
		}
		return "Seeking..."
	}
	if !m.PlayingVideo || m.Paused {
		return ""
	}
	if !m.ProgressSeen && !m.VideoStarted {
		return "Loading..."
	}
	if m.Buffering || (!m.BufferingKnown && now.Sub(m.LastAdvance) >= 3*time.Second) {
		return "Buffering..."
	}
	return ""
}

// finishSeekControls starts the normal timeout once seeking has settled.
func (m *playbackState) finishSeekControls(now time.Time) {
	if m.SeekControls {
		m.SeekControls = false
		m.RevealControls(now)
	}
}

// ToggleControls handles a show/hide action, including a pinned seek menu.
func (m *playbackState) ToggleControls(now time.Time) {
	if m.ControlsVisible(now) {
		m.HideControls()
	} else {
		m.RevealControls(now)
		m.SeekControls = m.SeekTarget != nil || (m.PlayingVideo && !m.ProgressSeen && !m.VideoStarted)
	}
}
