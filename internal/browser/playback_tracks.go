package browser

import (
	"fmt"
	"time"

	"misterfin-go/internal/jellyfin"
	"misterfin-go/internal/playback"
)

// trackPicker owns navigation within the recorded-video Options menu.
// Directions navigate this menu while it is open. They toggle controls otherwise.
type trackPicker struct {
	visible  bool
	tab      int
	selected [3]int
}

// TrackRow is one display choice. Index is a server stream index or picture mode.
type TrackRow struct {
	Index  int
	Label  string
	Active bool
}

// TrackMenu is an immutable render snapshot, independent of the input device.
type TrackMenu struct {
	Tab, Selected int
	Rows          []TrackRow
	Delay         string
	Message       string
}

func (c *PlaybackController) hasTracks() bool {
	if !c.running || c.item.Type == "Audio" || jellyfin.IsLive(c.item) {
		return false
	}
	return true
}

func (c *PlaybackController) trackRows(tab int) []TrackRow {
	if tab == 2 {
		return []TrackRow{
			{int(playback.PictureOriginal), "Original", c.tracks.Picture == playback.PictureOriginal},
			{int(playback.PictureZoom43), "Zoom", c.tracks.Picture == playback.PictureZoom43},
		}
	}
	kind, index, label := "Subtitle", c.tracks.Selection.SubtitleIndex, "Off"
	if tab == 1 {
		kind, index, label = "Audio", c.tracks.Selection.AudioIndex, "Server default"
	}
	rows := []TrackRow{{Index: -1, Label: label, Active: index < 0}}
	for _, stream := range c.tracks.Streams {
		if stream.Type == kind {
			rows = append(rows, TrackRow{stream.Index, stream.Label(), stream.Index == index})
		}
	}
	return rows
}

func (c *PlaybackController) openTracks() {
	c.state.HideControls()
	c.picker.visible = true
	for tab := 0; tab < len(c.picker.selected); tab++ {
		for i, row := range c.trackRows(tab) {
			if row.Active {
				c.picker.selected[tab] = i
			}
		}
	}
	c.notice = ""
}

// trackKey consumes picker navigation without changing the normal playback map.
func (c *PlaybackController) trackKey(key string, now time.Time) {
	switch key {
	case "back", "select":
		c.picker.visible = false
		c.state.HideControls()
	case "previous":
		c.picker.tab = max(0, c.picker.tab-1)
	case "next":
		c.picker.tab = min(len(c.picker.selected)-1, c.picker.tab+1)
	case "up", "down":
		delta := 1
		if key == "up" {
			delta = -1
		}
		c.picker.selected[c.picker.tab] = max(0, min(len(c.trackRows(c.picker.tab))-1, c.picker.selected[c.picker.tab]+delta))
	case "seek-backward", "seek-forward":
		// While choosing subtitles, triggers adjust text timing rather than seeking.
		sub, ok := c.tracks.Stream("Subtitle", c.tracks.Selection.SubtitleIndex)
		if c.picker.tab == 0 && ok && sub.TextSubtitle() && c.tracks.ClientSubtitles {
			delta := 100 * time.Millisecond
			if key == "seek-backward" {
				delta = -delta
			}
			c.subtitleDelay = max(-10*time.Second, min(10*time.Second, c.subtitleDelay+delta))
		}
	case "open":
		c.applyTrack(now)
	}
}

func (c *PlaybackController) applyTrack(now time.Time) {
	rows := c.trackRows(c.picker.tab)
	index := rows[c.picker.selected[c.picker.tab]].Index
	if c.picker.tab == 2 && c.tracks.LivePicture {
		c.applyPicture(playback.PictureMode(index))
		return
	}
	if rows[c.picker.selected[c.picker.tab]].Active && !c.subtitleLoading {
		if c.picker.tab == 2 {
			return
		}
		c.picker.visible = false
		c.state.HideControls()
		return
	}
	if !c.state.ProgressSeen || c.seekPhase != seekInactive || c.state.SeekTarget != nil {
		c.notice = "Wait for playback before changing options"
		return
	}
	if c.subtitleLoading && c.picker.tab != 0 {
		c.notice = "Wait for subtitles before changing options"
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
		if c.tracks.ClientSubtitles && (!oldOK || old.TextSubtitle()) && (index < 0 || subOK && sub.TextSubtitle()) {
			request := c.subtitleRequest + 1
			select {
			case c.controls <- playback.Control{Kind: "subtitle", Index: index, Request: request}:
				c.subtitleRequest = request
				c.subtitleLoading = true
				c.notice = "Loading subtitles..."
			default:
				c.notice = "Player is busy. Try again."
			}
			return
		}
	}
	c.subtitleRequest++
	c.subtitleLoading = false
	c.trackOptions = options
	// Picture comparisons keep the Options menu open, including on backends
	// that still need a stream replacement. Track selection retains its close.
	c.picker.visible = c.picker.tab == 2
	c.notice = ""
	// Reuse the tested replacement gate and pause restoration at the current time.
	target := c.state.PositionTicks
	c.state.SwitchingTracks = true
	c.state.SeekTarget = &target
	c.state.SeekDeadline = now
	c.state.SeekControls = false
	c.Tick(now)
}

// applyPicture leaves playback and menu visibility alone. Update the active
// marker only after the decoder confirms the most recent request.
func (c *PlaybackController) applyPicture(mode playback.PictureMode) {
	if !c.state.ProgressSeen || c.seekPhase != seekInactive || c.state.SeekTarget != nil {
		return
	}
	if !c.picturePending && c.tracks.Picture == mode {
		return
	}
	request := c.pictureRequest + 1
	select {
	case c.controls <- playback.Control{Kind: "picture", Picture: mode, Request: request}:
		c.pictureRequest = request
		c.picturePending = true
		c.trackOptions.Picture = mode
		c.notice = ""
	default:
		c.notice = "Player is busy. Try again."
	}
}

func (c *PlaybackController) trackPresentation(p *PlaybackPresentation, now time.Time) {
	p.TracksAvailable = c.hasTracks()
	if c.picker.visible {
		p.Tracks = &TrackMenu{Tab: c.picker.tab, Selected: c.picker.selected[c.picker.tab], Rows: c.trackRows(c.picker.tab), Message: c.notice}
		if c.picker.tab == 2 && p.Tracks.Message == "" {
			p.Tracks.Message = "Original aspect ratio, no cropping"
			if c.picker.selected[2] == 1 {
				p.Tracks.Message = "Enlarge the picture, crop the sides"
			}
		}
		sub, ok := c.tracks.Stream("Subtitle", c.tracks.Selection.SubtitleIndex)
		if c.picker.tab == 0 && ok && sub.TextSubtitle() && c.tracks.ClientSubtitles {
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
	p.Subtitle = c.tracks.Text.At(ticks - int64(c.subtitleDelay/100))
}
