package browser

// music draws the current track, elapsed time, and optional controls.
func (p *screenPainter) music() {
	c := p.canvas
	art := p.scene.Artwork
	w, h := p.width, p.height
	sy := p.safeY
	bottom := p.bottom
	v := &p.scene.View
	s := p.scene

	rows := controlRows(w, []controlHint{
		hint(s.Controls, "track-previous", "Previous"), hint(s.Controls, "track-next", "Next"),
		hint(s.Controls, "seek-backward", "-10s"), hint(s.Controls, "seek-forward", "+10s"),
	}, playbackHints(s.Controls, s.Playback.Paused))
	menuTop := bottom - max(0, len(rows)-1)*controlRowHeight - 6
	progressY := menuTop - 12
	titleY := progressY - 32
	p.header("Now playing")
	c.Image(art.Primary, 24, sy+28, w-48, max(1, titleY-10-(sy+28)))
	center(c, titleY, truncate(v.Detail.Name, w-48, 1), titleColor, 1)
	center(c, progressY-16, runtime(s.Playback.PositionTicks)+" / "+runtime(v.Detail.RunTimeTicks), dimColor, 1)
	c.Rect(24, progressY, w-48, 3, 0x303030)
	if v.Detail.RunTimeTicks > 0 {
		c.Rect(24, progressY, int(min(s.Playback.PositionTicks, v.Detail.RunTimeTicks)*int64(w-48)/v.Detail.RunTimeTicks), 3, titleColor)
	}
	if s.Playback.ControlsVisible {
		c.Shade(0, menuTop, w, h-menuTop, 210)
		drawControls(c, bottom, rows)
	}
	if s.Notice != "" {
		center(c, bottom, s.Notice, dimColor, 1)
	}
}
