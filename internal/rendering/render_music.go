package rendering

import (
	"image"
	"strings"

	"misterfin-crt/internal/input/control"
)

// music draws the current track, elapsed time, and optional controls.
func (p *screenPainter) music() {
	c := p.canvas
	art := p.scene.Artwork
	w, h := p.width, p.height
	sy := p.safeY
	bottom := p.bottom
	v := &p.scene.Content
	s := p.scene

	actions := playbackHints(s.Controls, s.Playback.Paused)
	if s.Music != nil {
		actions = append(actions, hint(s.Controls, control.Select, "Background"))
	}
	rows := controlRows(w, []controlHint{
		hint(s.Controls, control.TrackPrevious, "Previous"), hint(s.Controls, control.TrackNext, "Next"),
		hint(s.Controls, control.SeekBackward, "-10s"), hint(s.Controls, control.SeekForward, "+10s"),
	}, actions)
	menuTop := bottom - max(0, len(rows)-1)*controlRowHeight - 6
	progressY := menuTop - 12
	meterY := progressY - 15
	meters := s.Music != nil && s.Music.Config.Meters && p.visualizer != nil
	if meters {
		progressY -= 20
	}
	titleY := progressY - 32
	artist := strings.Join(v.Detail.Artists, ", ")
	if v.Detail.Album != "" {
		if artist != "" {
			artist += " - "
		}
		artist += v.Detail.Album
	}
	if artist != "" {
		titleY -= 10
	}
	spinning := false
	if s.Music != nil && p.visualizer != nil {
		s.MusicFrame.ArtworkBounds = image.Rect(24, sy+28, w-24, max(sy+29, titleY-10))
		p.visualizer.Draw(c, s.Music, s.MusicIndex, s.MusicFrame)
		spinning = s.Music.Config.Backgrounds[s.MusicIndex].Type == "spinning"
		c.Shade(0, 0, w, sy+22, 155)
		c.Shade(0, titleY-4, w, h-titleY+4, 175)
	}
	if meters {
		p.visualizer.Meters(c, 24, meterY, w-48)
	}
	title := "Now playing"
	if s.Shuffle {
		title = "Shuffle"
	}
	if s.MusicLabel && s.Music != nil {
		title = s.Music.Config.Backgrounds[s.MusicIndex].Name
	}
	p.header(title, p.safeY)
	if s.MusicMessage != "" {
		center(c, sy+19, s.MusicMessage, dimColor, 1)
	}
	if !spinning {
		c.Image(art.Primary, 24, sy+28, w-48, max(1, titleY-10-(sy+28)))
	}
	center(c, titleY, truncate(v.Detail.Name, w-48, 1), titleColor, 1)
	if artist != "" {
		center(c, progressY-28, truncate(artist, w-48, 1), dimColor, 1)
	}
	center(c, progressY-16, runtime(s.Playback.PositionTicks)+" / "+runtime(v.Detail.RunTimeTicks), dimColor, 1)
	c.Rect(24, progressY, w-48, 3, 0x303030)
	if v.Detail.RunTimeTicks > 0 {
		c.Rect(24, progressY, int(min(s.Playback.PositionTicks, v.Detail.RunTimeTicks)*int64(w-48)/v.Detail.RunTimeTicks), 3, titleColor)
	}
	if s.Playback.ControlsVisible {
		top := controlsTop(bottom, rows) - 3
		c.Shade(0, top, w, h-top, 210)
		drawControls(c, bottom, rows)
	}
	if s.Notice != "" {
		center(c, bottom, s.Notice, dimColor, 1)
	}
}
