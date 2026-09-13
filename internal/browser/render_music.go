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

	p.header("Now playing")
	c.Image(art.Primary, 24, sy+28, w-48, h-sy-98)
	center(c, h-sy-62, truncate(v.Detail.Name, w-48, 1), titleColor, 1)
	center(c, h-sy-46, runtime(s.Playback.PositionTicks)+" / "+runtime(v.Detail.RunTimeTicks), dimColor, 1)
	c.Rect(24, h-sy-30, w-48, 3, 0x303030)
	if v.Detail.RunTimeTicks > 0 {
		c.Rect(24, h-sy-30, int(min(s.Playback.PositionTicks, v.Detail.RunTimeTicks)*int64(w-48)/v.Detail.RunTimeTicks), 3, titleColor)
	}
	if s.Playback.ControlsVisible {
		c.Shade(0, bottom-22, w, h-bottom+22, 210)
		action := "B:pause"
		if s.Playback.Paused {
			action = "B:play"
		}
		center(c, bottom-12, "LEFT/RIGHT: previous/next track", dimColor, 1)
		center(c, bottom, action+"   A:stop", dimColor, 1)
	}
	if s.Notice != "" {
		center(c, bottom, s.Notice, dimColor, 1)
	}
}
