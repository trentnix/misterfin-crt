package browser

import (
	"time"

	"mistervision/internal/media"
	"mistervision/internal/playback"
)

// wantsPause returns user intent, excluding the temporary pause used for seeking.
func (c *PlaybackController) wantsPause() bool {
	return c.pauseRequested
}

// SetPaused sets explicit pause intent independently of the visible menu. The
// decoder receives an idempotent command, so repeated remote Pause cannot resume.
// A busy command channel retains the latest intent for Tick to retry.
func (c *PlaybackController) SetPaused(paused bool) {
	if !c.running || c.stoppedByUser {
		return
	}
	c.pauseRequested = paused
	if c.seekPhase != seekInactive || !c.state.ProgressSeen {
		// A seek holds the original paused. The replacement applies user intent
		// on its first position update, once its controls are ready.
		return
	}
	c.pendingPause = playback.Resume
	if paused {
		c.pendingPause = playback.SetPaused
	}
	c.deliverPause()
}

// deliverPause retries the latest explicit pause intent without blocking the loop.
// Visible state updates immediately, but later feedback cannot change user intent.
func (c *PlaybackController) deliverPause() bool {
	if c.pendingPause == "" {
		return true
	}
	if !c.sendCommand(c.pendingPause) {
		return false
	}
	c.state.Paused = c.pendingPause == playback.SetPaused
	c.pendingPause = ""
	return true
}

// SeekTo uses the same video handoff as local seeking and a relative decoder
// operation for audio. Live TV cannot seek. Targets are clamped to the duration.
func (c *PlaybackController) SeekTo(target int64, now time.Time) {
	if !c.running || c.stoppedByUser || !c.state.ProgressSeen || media.IsLive(c.item) {
		return
	}
	target = max(0, target)
	if c.item.RunTimeTicks > 0 {
		target = min(target, max(0, c.item.RunTimeTicks-10000000))
	}
	if c.item.Type == "Audio" {
		seconds := (target - c.state.PositionTicks) / 10000000
		if seconds > 2147483647 || seconds < -2147483648 {
			return
		}
		select {
		case c.active.controls <- playback.Control{Kind: playback.SeekAudioRelative, Seconds: int(seconds)}:
		default:
		}
		return
	}
	c.picker.visible = false
	if c.seekPhase != seekInactive {
		c.cancelPendingSeek()
		c.seekPhase = seekRetargeting
	}
	c.state.SeekControls = c.state.ControlsVisible(now)
	c.state.SeekTarget = &target
	c.state.SeekPresses = 2
	c.state.SeekInFlight = false
	c.state.SwitchingTracks = false
	c.state.SeekDeadline = now.Add(500 * time.Millisecond)
}
