package browser

import (
	"fmt"
	"time"

	"mistervision/internal/input/control"
	"mistervision/internal/media"
	"mistervision/internal/playback"
	"mistervision/internal/rendering"
)

// trackPicker owns navigation within the video Options menu.
// Directions navigate this menu while it is open. They toggle controls otherwise.
type trackPicker struct {
	visible  bool
	tab      int
	selected [3]int
}

func (c *PlaybackController) hasTracks() bool {
	if !c.running || c.item.Type == "Audio" {
		return false
	}
	return true
}

func (c *PlaybackController) trackRows(tab int) []rendering.TrackRow {
	if media.IsLive(c.item) {
		if tab == 0 {
			return c.captions.rows()
		}
		if tab == 1 && !c.tracks.LiveAudio {
			return nil
		}
	}
	if tab == 2 {
		if media.IsLive(c.item) && !c.tracks.LivePicture {
			return []rendering.TrackRow{{Index: int(playback.PictureOriginal), Label: "Original", Active: true}}
		}
		return []rendering.TrackRow{
			{Index: int(playback.PictureOriginal), Label: "Original", Active: c.tracks.Picture == playback.PictureOriginal},
			{Index: int(playback.PictureZoom43), Label: "Zoom", Active: c.tracks.Picture == playback.PictureZoom43},
		}
	}
	kind, index, label := "Subtitle", c.tracks.Selection.SubtitleIndex, "Off"
	if tab == 1 {
		kind, index, label = "Audio", c.tracks.Selection.AudioIndex, "Server default"
	}
	rows := []rendering.TrackRow{{Index: -1, Label: label, Active: index < 0}}
	for _, stream := range c.tracks.Streams {
		if stream.Type == kind {
			rows = append(rows, rendering.TrackRow{Index: stream.Index, Label: stream.Label(), Active: stream.Index == index})
		}
	}
	return rows
}

func (c *PlaybackController) openTracks() {
	c.state.HideControls()
	c.picker.visible = true
	for tab := 0; tab < len(c.picker.selected); tab++ {
		c.picker.selected[tab] = 0
		for i, row := range c.trackRows(tab) {
			if row.Active {
				c.picker.selected[tab] = i
			}
		}
	}
	c.notice = ""
}

// trackKey consumes picker navigation without changing the normal playback map.
func (c *PlaybackController) trackKey(key control.Action, now time.Time) {
	switch key {
	case control.Back, control.Select:
		c.picker.visible = false
		c.state.HideControls()
	case control.Previous:
		c.picker.tab = max(0, c.picker.tab-1)
	case control.Next:
		c.picker.tab = min(len(c.picker.selected)-1, c.picker.tab+1)
	case control.Up, control.Down:
		delta := 1
		if key == control.Up {
			delta = -1
		}
		c.picker.selected[c.picker.tab] = max(0, min(len(c.trackRows(c.picker.tab))-1, c.picker.selected[c.picker.tab]+delta))
	case control.SeekBackward, control.SeekForward:
		// While choosing subtitles, triggers adjust text timing rather than seeking.
		sub, ok := c.tracks.Stream("Subtitle", c.tracks.Selection.SubtitleIndex)
		if c.picker.tab == 0 && ok && sub.ClientSubtitle() && c.tracks.ClientSubtitles {
			delta := 100 * time.Millisecond
			if key == control.SeekBackward {
				delta = -delta
			}
			c.subtitleDelay = max(-10*time.Second, min(10*time.Second, c.subtitleDelay+delta))
		}
	case control.Open:
		c.applyTrack(now)
	}
}

func (c *PlaybackController) applyTrack(now time.Time) {
	rows := c.trackRows(c.picker.tab)
	if len(rows) == 0 {
		return
	}
	// Keep selection within the current tab if stream metadata changes.
	c.picker.selected[c.picker.tab] = min(c.picker.selected[c.picker.tab], len(rows)-1)
	index := rows[c.picker.selected[c.picker.tab]].Index
	if media.IsLive(c.item) && c.picker.tab == 0 {
		c.captions.enabled = index == 0
		c.picker.visible = false
		c.state.HideControls()
		c.notice = ""
		return
	}
	if c.picker.tab == 2 && c.tracks.LivePicture {
		c.applyPicture(playback.PictureMode(index))
		return
	}
	if rows[c.picker.selected[c.picker.tab]].Active && !c.subtitleLoading {
		c.picker.visible = false
		c.state.HideControls()
		return
	}
	if !c.state.ProgressSeen || c.seekPhase != seekInactive || c.state.SeekTarget != nil {
		c.notice = messageWaitForPlayback
		return
	}
	if c.subtitleLoading && c.picker.tab != 0 {
		c.notice = messageWaitForSubtitles
		return
	}
	options := c.trackOptions
	if c.picker.tab == 2 {
		options.Picture = playback.PictureMode(index)
	} else if c.picker.tab == 1 {
		options.Selection.AudioIndex = index
	} else {
		options.Selection.SubtitleIndex = index
		options.Text = nil
		old, oldOK := c.tracks.Stream("Subtitle", c.tracks.Selection.SubtitleIndex)
		sub, subOK := c.tracks.Stream("Subtitle", index)
		if c.tracks.ClientSubtitles && (!oldOK || old.ClientSubtitle()) && (index < 0 || subOK && sub.ClientSubtitle()) {
			request := c.subtitleRequest + 1
			select {
			case c.active.controls <- playback.Control{Kind: playback.SelectSubtitle, Index: index, Request: request}:
				c.subtitleRequest = request
				c.subtitleLoading = true
				c.notice = "Loading subtitles..."
			default:
				c.notice = messagePlayerBusy
			}
			return
		}
	}
	c.subtitleRequest++
	c.subtitleLoading = false
	c.trackOptions = options
	c.picker.visible = false
	c.notice = ""
	// Reuse the replacement gate. Live audio changes reopen at the live edge.
	target := c.state.PositionTicks
	if media.IsLive(c.item) {
		target = 0
	}
	c.state.SwitchingTracks = true
	c.state.SeekTarget = &target
	c.state.SeekDeadline = now
	c.state.SeekControls = false
	c.Tick(now)
}

// applyPicture leaves playback running. A successful acknowledgment dismisses
// the picker. Failed requests leave it open so the user can retry.
func (c *PlaybackController) applyPicture(mode playback.PictureMode) {
	if !c.state.ProgressSeen || c.seekPhase != seekInactive || c.state.SeekTarget != nil {
		return
	}
	if !c.picturePending && c.tracks.Picture == mode {
		c.picker.visible = false
		c.state.HideControls()
		return
	}
	request := c.pictureRequest + 1
	select {
	case c.active.controls <- playback.Control{Kind: playback.SetPicture, Picture: mode, Request: request}:
		c.pictureRequest = request
		c.picturePending = true
		c.trackOptions.Picture = mode
		c.notice = ""
	default:
		c.notice = messagePlayerBusy
	}
}

func (c *PlaybackController) trackPresentation(p *rendering.PlaybackPresentation, now time.Time) {
	p.TracksAvailable = c.hasTracks()
	if c.picker.visible {
		rows := c.trackRows(c.picker.tab)
		selected := max(0, min(c.picker.selected[c.picker.tab], len(rows)-1))
		p.Tracks = &rendering.TrackMenu{Tab: c.picker.tab, Selected: selected, Rows: rows, Message: c.notice}
		if p.Tracks.Message == "" {
			p.Tracks.Message = c.trackMessage(c.picker.tab, selected)
		}
		sub, ok := c.tracks.Stream("Subtitle", c.tracks.Selection.SubtitleIndex)
		if c.picker.tab == 0 && ok && sub.ClientSubtitle() && c.tracks.ClientSubtitles {
			p.Tracks.Delay = fmt.Sprintf("Subtitle delay: %+.1fs", c.subtitleDelay.Seconds())
		}
	}
	if !c.running || !c.state.ProgressSeen || c.state.SeekTarget != nil {
		return
	}
	ticks := c.state.PositionTicks
	if !c.state.Paused && p.WaitLabel == "" {
		ticks += int64(min(time.Second, max(0, now.Sub(c.state.LastAdvance))) / 100)
	}
	if media.IsLive(c.item) {
		if c.captions.enabled {
			p.Subtitle = c.captions.text
		}
		return
	}
	p.Subtitle = c.tracks.Text.At(ticks - int64(c.subtitleDelay/100))
}

// trackMessage explains the selected picture mode or an unavailable live option.
func (c *PlaybackController) trackMessage(tab, selected int) string {
	if media.IsLive(c.item) {
		switch tab {
		case 0:
			if !c.captions.available {
				return messageNoCaptionData
			}
			return ""
		case 1:
			if !c.tracks.LiveAudio {
				return messageLiveAudioUnavailable
			}
			return "Changing audio briefly reloads the channel."
		case 2:
			if !c.tracks.LivePicture {
				return messageLivePictureUnavailable
			}
		}
	}
	if tab == 2 {
		if selected == 1 {
			return "Enlarge the picture and crop the edges."
		}
		return "Original aspect ratio, no cropping"
	}
	return ""
}
