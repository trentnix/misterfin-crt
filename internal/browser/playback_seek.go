package browser

import (
	"time"

	"mistervision/internal/input/control"
	"mistervision/internal/playback"
)

// seekPhase describes a replacement stream's lifecycle. The initial half-second
// destination preview uses seekInactive with state.SeekTarget set. Once the
// original has been paused, seekRetargeting keeps that pause while a new target
// waits for its deadline. This avoids treating the automatic pause as user intent.
type seekPhase uint8

const (
	seekInactive    seekPhase = iota
	seekPreparing             // Request in progress, or ready and waiting for the old decoder.
	seekRetargeting           // Obsolete request canceled. Destination preview is visible.
)

// Tick retries pending pause intent and starts preparation after a seek settles for 0.5s.
// It does not restart an already preparing request on each animation tick.
func (c *PlaybackController) Tick(now time.Time) {
	if !c.running || c.stoppedByUser {
		return
	}
	if !c.deliverPause() || c.state.SeekTarget == nil {
		return
	}
	if c.seekPhase == seekPreparing || now.Before(c.state.SeekDeadline) {
		return
	}
	if c.seekPhase == seekInactive {
		// A failed delivery leaves the seek in its preview phase. Retry on the
		// next tick before preparing a replacement or claiming we paused video.
		paused := c.wantsPause()
		if !paused && !c.sendCommand(playback.SetPaused) {
			return
		}
		c.subtitleRequest++
		c.subtitleLoading = false
		c.notice = ""
	}
	c.seekPhase = seekPreparing
	c.state.SeekInFlight = true
	c.launchPendingSeek(*c.state.SeekTarget)
}

// retargetSeek returns from Seeking to the destination preview. The original
// pause preference survives cancellation and the renewed half-second delay.
func (c *PlaybackController) retargetSeek(key control.Action, now time.Time) {
	c.state.SwitchingTracks = false
	c.state.seekVideo(&c.item, key, now)
	if c.state.SeekTarget == nil {
		return
	}
	c.cancelPendingSeek()
	c.seekPhase = seekRetargeting
	c.state.SeekInFlight = false
}

func (c *PlaybackController) cancelPendingSeek() {
	c.pending.stopWithAsyncCleanup()
	// Late ready/end events carry the old ID and will now be ignored.
	c.pending = playbackProcess{}
}

func (c *PlaybackController) launchPendingSeek(target int64) {
	c.cancelPendingSeek()
	c.pendingTarget = target
	gate := make(chan struct{})
	// The offset belongs to this request. Later retargets must not mutate it.
	c.pending = c.launch(c.item, &target, gate, true, c.trackOptions)
	c.pending.gate = gate
}

// activatePendingSeek requires a ready replacement and no active decoder.
// The gate opens only after the old decoder releases the display and audio.
func (c *PlaybackController) activatePendingSeek(now time.Time) {
	c.active = c.pending
	c.subtitleRequest = 0
	c.picturePending = false
	c.subtitleLoading = false
	if c.active.tracks != nil {
		c.tracks = *c.active.tracks
		c.trackOptions = c.tracks.TrackOptions
	}
	c.pending = playbackProcess{}
	c.active.allowStart()
	c.clearSeek()
	c.state.SeekPresses = 0
	c.state.Paused = false
	c.state.PositionTicks = c.pendingTarget
	c.state.ProgressSeen = false
	c.state.VideoStarted = false
	c.state.LastAdvance = now
	c.state.Buffering = false
	c.state.BufferingKnown = false
}

func (c *PlaybackController) clearSeek() {
	c.seekPhase = seekInactive
	c.state.SwitchingTracks = false
	c.state.SeekTarget = nil
	c.state.SeekInFlight = false
}

// replacementFailed restores the latest user intent on the old decoder.
// If that decoder already ended, the browser returns to its non-video display.
func (c *PlaybackController) replacementFailed(err error, now time.Time) {
	c.state.finishSeekControls(now)
	c.trackOptions = c.tracks.TrackOptions
	originalEnded := c.active.id == 0
	resume := !c.pauseRequested && c.running && !originalEnded
	c.pending = playbackProcess{}
	c.clearSeek()
	if resume {
		c.SetPaused(false)
	}
	if err != nil {
		c.notice = err.Error()
	}
	if originalEnded {
		c.finishVideo()
	}
}
