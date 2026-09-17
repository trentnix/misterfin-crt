package browser

import (
	"time"

	"mistervision/internal/input/control"
	"mistervision/internal/media"
	"mistervision/internal/playback"
	"mistervision/internal/rendering"
)

// PlaybackController coordinates one item's playback on the browser event loop.
// Call its methods from that loop only. Decoder goroutines return PlaybackEvents
// through the launch bridge instead of mutating controller state.
//
// Library navigation and music queues belong to the browser. Drawing and framebuffer
// ownership belong to the renderer and output adapters.
type PlaybackController struct {
	// State stays private. Rendering receives a value snapshot.
	tracks          playback.VideoTracks
	trackOptions    playback.TrackOptions
	picker          trackPicker
	captions        captionState
	subtitleDelay   time.Duration
	subtitleRequest int
	subtitleLoading bool
	state           playbackState
	item            media.Item
	launch          playbackLaunch
	controls        chan playback.Control

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
	pictureRequest       int
	picturePending       bool
}

func newPlaybackController(launch playbackLaunch) *PlaybackController {
	return &PlaybackController{
		launch:   launch,
		controls: make(chan playback.Control, 16),
	}
}

// Start begins a new item after the preceding item has finished. A nil offset
// resumes from server's saved position. A pointer to zero requests a restart.
func (c *PlaybackController) Start(item media.Item, offset *int64, paused bool, now time.Time) {
	// Reopening immediately must not read the old server resume position while
	// the preceding Stop is still saving the position we already know locally.
	if offset == nil && item.ID == c.item.ID && c.stoppedByUser && !c.cleanupComplete && c.state.ProgressSeen && item.Type != "Audio" && !media.IsLive(item) {
		resume := c.state.PositionTicks
		offset = &resume
	}
	c.tracks = playback.VideoTracks{TrackOptions: playback.TrackOptions{Selection: media.TrackSelection{AudioIndex: -1, SubtitleIndex: -1}}}
	c.trackOptions = c.tracks.TrackOptions
	c.picker = trackPicker{}
	c.captions = captionState{}
	c.pictureRequest = 0
	c.picturePending = false
	c.subtitleDelay = 0
	c.subtitleRequest = 0
	c.subtitleLoading = false
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
	c.active = c.launch(item, offset, nil, false, c.controls, c.trackOptions)
	c.running = true
}

// Snapshot copies the visible playback state. Later events cannot change it.
func (c *PlaybackController) Snapshot(now time.Time) rendering.PlaybackPresentation {
	presentation := c.state.presentation(&c.item, now)
	presentation.Active = c.running
	presentation.Audio = c.item.Type == "Audio"
	presentation.Notice = c.notice
	c.trackPresentation(&presentation, now)
	return presentation
}

// Key receives normalized browser actions. During a seek, retargeting, menu toggling, and
// stopping are accepted. Music track navigation is handled by the browser.
func (c *PlaybackController) Key(key control.Action, now time.Time) {
	if c.picker.visible {
		c.trackKey(key, now)
		return
	}
	if key == control.Select && c.hasTracks() && c.seekPhase == seekInactive {
		c.openTracks()
		return
	}
	if c.seekPhase != seekInactive && key != control.Back && key != control.ToggleControls {
		if !media.IsLive(c.item) && (key == control.SeekBackward || key == control.SeekForward) {
			c.retargetSeek(key, now)
		}
		return
	}
	switch key {
	case control.SeekBackward, control.SeekForward:
		if c.item.Type == "Audio" {
			c.seekAudio(key)
		} else {
			c.state.seekVideo(&c.item, key, now)
		}
	case control.Back:
		c.stopByUser()
	case control.Open:
		c.state.HideControls()
		if c.pauseOnFirstPosition {
			// The user requested resume before the replacement's first position.
			c.pauseOnFirstPosition = false
		} else {
			c.sendCommand(playback.TogglePause)
		}
	case control.ToggleControls:
		c.state.ToggleControls(now)
	}
}

func (c *PlaybackController) stopByUser() {
	c.picker.visible = false
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
	c.sendCommand(playback.Refresh)
}

// sendCommand never blocks the UI loop. The caller can retry on a later event
// when delivery is required, as with pause restoration on a position update.
func (c *PlaybackController) sendCommand(kind playback.ControlKind) bool {
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
func (c *PlaybackController) seekAudio(key control.Action) {
	if !c.running || !c.state.ProgressSeen {
		return
	}
	seconds := 10
	if key == control.SeekBackward {
		seconds = -seconds
	}
	select {
	case c.controls <- playback.Control{Kind: playback.SeekAudioStep, Seconds: seconds}:
	default:
	}
}
