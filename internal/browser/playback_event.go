package browser

import (
	"time"

	"misterfin-go/internal/playback"
)

// PlaybackEvent carries decoder feedback without browser navigation or artwork.
type PlaybackEvent struct {
	Picture  playback.PictureResult
	Tracks   playback.VideoTracks
	Subtitle playback.SubtitleResult
	Kind     PlaybackEventKind
	ID       int // Decoder generation assigned by the launch bridge.
	Levels   playback.AudioLevels
	Ticks    int64 // Position including the requested stream offset.
	Value    bool  // Used by PlaybackPaused and PlaybackBuffering.
	Err      error // Used by PlaybackEnded.
}

// PlaybackEventKind identifies which fields of [PlaybackEvent] are meaningful.
type PlaybackEventKind uint8

const (
	PlaybackPrepared PlaybackEventKind = iota
	PlaybackPosition
	PlaybackPaused
	PlaybackBuffering
	PlaybackEnded
	PlaybackVideoStarted
	PlaybackControlFailed
	PlaybackLevels
	PlaybackCleanupDone
	PlaybackTrackInfo
	PlaybackSubtitle
	PlaybackPicture
)

// Handle applies feedback from a tracked decoder. It returns true only when the
// active item ends and the browser should advance a track or navigate away.
// A completed seek handoff does not complete the item.
func (c *PlaybackController) Handle(event PlaybackEvent, now time.Time) bool {
	if event.ID == 0 {
		return false
	}
	switch event.Kind {
	case PlaybackPicture:
		if !c.running || c.stoppedByUser || event.ID != c.active.id || event.Picture.Request != c.pictureRequest || c.seekPhase != seekInactive {
			return false
		}
		c.picturePending = false
		if event.Picture.Err != nil {
			c.trackOptions.Picture = c.tracks.Picture
			c.notice = event.Picture.Err.Error()
		} else {
			c.tracks.Picture = event.Picture.Mode
			c.trackOptions.Picture = event.Picture.Mode
			c.notice = ""
			c.picker.visible = false
		}
	case PlaybackTrackInfo:
		if event.ID == c.pending.id {
			info := event.Tracks
			c.pending.tracks = &info
		} else if c.running && event.ID == c.active.id {
			c.tracks = event.Tracks
			c.trackOptions = c.tracks.TrackOptions
		}
	case PlaybackSubtitle:
		if !c.running || c.stoppedByUser || event.ID != c.active.id || c.seekPhase != seekInactive || c.state.SeekTarget != nil || event.Subtitle.Request != c.subtitleRequest {
			return false
		}
		c.subtitleLoading = false
		if event.Subtitle.Err != nil {
			c.notice = event.Subtitle.Err.Error()
		} else {
			c.tracks.Selection.SubtitleIndex = event.Subtitle.Index
			c.tracks.Text = event.Subtitle.Text
			picture := c.trackOptions.Picture
			c.trackOptions = c.tracks.TrackOptions
			if c.picturePending {
				c.trackOptions.Picture = picture
			}
			c.notice = ""
			c.picker.visible = false
		}
	case PlaybackCleanupDone:
		if event.ID == c.active.id {
			c.cleanupComplete = true
		}
	case PlaybackPrepared:
		c.replacementReady(event.ID, now)
	case PlaybackEnded:
		return c.decoderEnded(event, now)
	default:
		// Playback feedback belongs only to the current decoder.
		if !c.running || event.ID != c.active.id {
			return false
		}
		switch event.Kind {
		case PlaybackVideoStarted:
			if !c.state.VideoStarted && c.state.PlayingVideo {
				c.state.VideoStarted = true
				if !c.state.ProgressSeen {
					c.state.LastAdvance = now
					if c.state.SeekTarget == nil {
						c.state.finishSeekControls(now)
					}
				}
			}
		case PlaybackControlFailed:
			if event.Err != nil {
				c.notice = event.Err.Error()
			}
		case PlaybackPaused:
			c.state.Paused = event.Value
			c.state.LastAdvance = now
		case PlaybackBuffering:
			c.state.Buffering = event.Value
			c.state.BufferingKnown = true
		case PlaybackPosition:
			c.updatePosition(event.Ticks, now)
		}
	}
	return false
}

func (c *PlaybackController) replacementReady(id int, now time.Time) {
	if id != c.pending.id {
		return
	}
	c.pending.ready = true
	if c.active.id == 0 {
		c.activatePendingSeek(now)
	} else {
		// Keep the replacement gated until the original's end event arrives.
		c.active.stopWithAsyncCleanup()
	}
}

func (c *PlaybackController) updatePosition(ticks int64, now time.Time) {
	if !c.state.ProgressSeen && c.state.SeekTarget == nil {
		c.state.finishSeekControls(now)
	}
	if !c.state.ProgressSeen || ticks != c.state.PositionTicks {
		c.state.LastAdvance = now
	}
	c.state.ProgressSeen = true
	c.state.PositionTicks = ticks
	if c.pauseOnFirstPosition && c.sendCommand("pause") {
		c.pauseOnFirstPosition = false
	}
}

func (c *PlaybackController) decoderEnded(event PlaybackEvent, now time.Time) bool {
	if event.ID == c.pending.id {
		// A pending process cannot finish normally: it is still behind its gate.
		c.replacementFailed(event.Err, now)
		return false
	}
	if event.ID != c.active.id {
		return false
	}
	if c.seekPhase != seekInactive && !c.stoppedByUser {
		c.active = playbackProcess{}
		// Retargeting can leave no replacement while the debounce runs. Do not
		// navigate away or open a gate until the latest replacement is ready.
		if c.pending.id != 0 && c.pending.ready {
			c.activatePendingSeek(now)
		}
		return false
	}
	c.clearSeek()
	c.running = false
	c.state.PlayingVideo = false
	// Keep the music menu through the gap while the browser finds another track.
	if c.item.Type != "Audio" || c.stoppedByUser || event.Err != nil {
		c.state.HideControls()
	}
	c.active.stop()
	c.notice = ""
	return true
}
