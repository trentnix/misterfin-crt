package browser

import "time"

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

// Tick starts preparation after the latest arrow press has settled for 0.5s.
// It does not restart an already preparing request on each animation tick.
func (c *PlaybackController) Tick(now time.Time) {
	if !c.running || c.state.SeekTarget == nil {
		return
	}
	if c.seekPhase == seekPreparing || now.Before(c.state.SeekDeadline) {
		return
	}
	if c.seekPhase == seekInactive {
		c.pausedBeforeSeek = c.state.Paused
		c.pausedForSeek = !c.state.Paused
		if c.pausedForSeek {
			c.sendCommand("pause")
		}
	}
	c.seekPhase = seekPreparing
	c.state.SeekInFlight = true
	c.launchPendingSeek(*c.state.SeekTarget)
}

// retargetSeek returns from Seeking to the destination preview. The original
// pause preference survives cancellation and the renewed half-second delay.
func (c *PlaybackController) retargetSeek(key string, now time.Time) {
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
	c.pending = c.launch(c.item, &target, gate, true, c.controls)
	c.pending.gate = gate
}

// activatePendingSeek requires a ready replacement and no active decoder.
// The gate opens only after the old decoder releases the display and audio.
func (c *PlaybackController) activatePendingSeek(now time.Time) {
	c.active = c.pending
	c.pending = playbackProcess{}
	c.active.allowStart()
	c.pauseOnFirstPosition = c.pausedBeforeSeek
	c.pausedForSeek = false
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
	c.state.SeekTarget = nil
	c.state.SeekInFlight = false
}

// replacementFailed resumes the old decoder only when the seek paused it.
// If that decoder already ended, the browser returns to its non-video display.
func (c *PlaybackController) replacementFailed(err error, now time.Time) {
	c.state.finishSeekControls(now)
	originalEnded := c.active.id == 0
	if c.pausedForSeek && c.running && !originalEnded {
		c.sendCommand("pause")
	}
	c.pausedForSeek = false
	c.pending = playbackProcess{}
	c.clearSeek()
	if err != nil {
		c.notice = err.Error() + "  A:back"
	}
	if originalEnded {
		c.finishVideo()
	}
}
