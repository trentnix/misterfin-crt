package browser

import (
	"time"

	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/playback"
)

// wantsPause returns user intent, excluding the temporary pause used for seeking.
func (c *PlaybackController) wantsPause() bool {
	if c.seekPhase != seekInactive {
		return c.pausedBeforeSeek
	}
	return c.pauseOnFirstPosition || c.state.Paused
}

// SetPaused sets explicit pause intent independently of the visible menu. The
// decoder receives an idempotent command, so repeated remote Pause cannot resume.
func (c *PlaybackController) SetPaused(paused bool) {
	if !c.running || c.stoppedByUser {
		return
	}
	if c.seekPhase != seekInactive {
		c.pausedBeforeSeek = paused
		c.pausedForSeek = !paused
		return
	}
	if !c.state.ProgressSeen {
		c.pauseOnFirstPosition = paused
		return
	}
	c.pauseOnFirstPosition = false
	kind := playback.Resume
	if paused {
		kind = playback.SetPaused
	}
	if c.sendCommand(kind) {
		c.state.Paused = paused
	}
}

// SeekTo uses the same video handoff as local seeking and a relative decoder
// operation for audio. Live TV cannot seek. Targets are clamped to the duration.
func (c *PlaybackController) SeekTo(target int64, now time.Time) {
	if !c.running || c.stoppedByUser || !c.state.ProgressSeen || jellyfin.IsLive(c.item) {
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
		case c.controls <- playback.Control{Kind: playback.SeekAudioRelative, Seconds: int(seconds)}:
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
