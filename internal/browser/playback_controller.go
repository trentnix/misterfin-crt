package browser

import (
	"time"

	"misterfin-go/internal/jellyfin"
	"misterfin-go/internal/playback"
)

// PlaybackController coordinates one item's playback on the browser event loop.
// Call its methods from that loop only. Decoder goroutines return PlaybackEvents
// through the launch bridge instead of mutating controller state.
//
// Navigation and track selection belong to the browser. Drawing and framebuffer
// ownership belong to the renderer and output adapters.
type PlaybackController struct {
	// State stays private. Rendering receives a value snapshot.
	state    playbackState
	item     jellyfin.Item
	launch   playbackLaunch
	controls chan playback.Control

	// The active decoder may be stopping while a replacement is being prepared.
	// A zero process ID means that slot has no decoder.
	active          playbackProcess
	pending         playbackProcess
	running         bool // Remains true across a seek, even between decoder processes.
	stoppedByUser   bool
	cleanupComplete bool // Final resume save has finished for the active process.

	seekPhase            seekPhase
	pendingTarget        int64 // Offset requested by the pending replacement.
	pausedBeforeSeek     bool  // User intent, captured before we pause for a seek.
	pausedForSeek        bool  // Whether we must resume the original if preparation fails.
	pauseOnFirstPosition bool  // Restore user pause after the replacement starts.
	notice               string
}

func newPlaybackController(launch playbackLaunch) *PlaybackController {
	return &PlaybackController{
		launch:   launch,
		controls: make(chan playback.Control, 16),
	}
}

// Start begins a new item after the preceding item has finished. A nil offset
// resumes from Jellyfin's saved position. A pointer to zero requests a restart.
func (c *PlaybackController) Start(item jellyfin.Item, offset *int64, paused bool, now time.Time) {
	// Reopening immediately must not read the old server resume position while
	// the preceding Stop is still saving the position we already know locally.
	if offset == nil && item.ID == c.item.ID && c.stoppedByUser && !c.cleanupComplete && c.state.ProgressSeen && item.Type != "Audio" && !jellyfin.IsLive(item) {
		resume := c.state.PositionTicks
		offset = &resume
	}
	c.item = item
	c.pauseOnFirstPosition = paused
	c.seekPhase = seekInactive
	c.stoppedByUser = false
	c.cleanupComplete = false
	c.notice = ""
	c.state = playbackState{
		PlayingVideo: item.Type != "Audio",
		LastAdvance:  now,
	}
	// Commands queued for the previous item must not reach the new player.
	c.controls = make(chan playback.Control, 16)
	c.active = c.launch(item, offset, nil, false, c.controls)
	c.running = true
}

// Snapshot copies the visible playback state. Later events cannot change it.
func (c *PlaybackController) Snapshot(now time.Time) PlaybackPresentation {
	presentation := c.state.presentation(&c.item, now)
	presentation.Active = c.running
	presentation.Audio = c.item.Type == "Audio"
	presentation.Notice = c.notice
	return presentation
}

// Key receives normalized browser actions. During a seek, retargeting, menu toggling, and
// stopping are accepted. Music track navigation is handled by the browser.
func (c *PlaybackController) Key(key string, now time.Time) {
	if c.seekPhase != seekInactive && key != "back" && key != "controls" {
		if key == "seek-backward" || key == "seek-forward" {
			c.retargetSeek(key, now)
		}
		return
	}
	switch key {
	case "seek-backward", "seek-forward":
		if c.item.Type == "Audio" {
			c.seekAudio(key)
		} else {
			c.state.seekVideo(&c.item, key, now)
		}
	case "back":
		c.stopByUser()
	case "open":
		c.state.HideControls()
		if c.pauseOnFirstPosition {
			// The user requested resume before the replacement's first position.
			c.pauseOnFirstPosition = false
		} else {
			c.sendCommand("pause")
		}
	case "controls":
		c.state.ToggleControls(now)
	}
}

func (c *PlaybackController) stopByUser() {
	c.stoppedByUser = true
	c.state.HideControls()
	c.clearSeek()
	c.cancelPendingSeek()
	c.active.stopWithAsyncCleanup()
	if c.active.id == 0 {
		// No completion event will arrive if the old decoder already stopped.
		c.finishVideo()
	}
}

// StopForTrackChange leaves queue selection and navigation with the browser.
func (c *PlaybackController) StopForTrackChange() {
	c.active.stop()
}

// Refresh requests a redraw of paused video after its overlay changes.
func (c *PlaybackController) Refresh() {
	c.sendCommand("refresh")
}

// sendCommand never blocks the UI loop. The caller can retry on a later event
// when delivery is required, as with pause restoration on a position update.
func (c *PlaybackController) sendCommand(kind string) bool {
	select {
	case c.controls <- playback.Control{Kind: kind}:
		return true
	default:
		return false
	}
}

// finishVideo ends decoder activity without changing browser navigation.
func (c *PlaybackController) finishVideo() {
	c.running = false
	c.state.PlayingVideo = false
	c.state.Paused = false
}

// Close cancels both tracked decoders before waiting, so a gated replacement
// cannot keep shutdown waiting for the original decoder to finish first.
func (c *PlaybackController) Close() {
	c.active.stop()
	c.pending.stop()
	c.active.wait()
	c.pending.wait()
}

// seekAudio uses the decoder's seekable audio source without replacing the
// player or changing pause state. Progress feedback supplies the actual position.
func (c *PlaybackController) seekAudio(key string) {
	if !c.running || !c.state.ProgressSeen {
		return
	}
	seconds := 10
	if key == "seek-backward" {
		seconds = -seconds
	}
	select {
	case c.controls <- playback.Control{Kind: "seek", Seconds: seconds}:
	default:
	}
}
