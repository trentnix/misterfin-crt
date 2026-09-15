package rendering

import (
	"misterfin-crt/internal/input/control"
)

// photo draws the full-screen image, optional navigation, and image errors.
func (p *screenPainter) photo() {
	c := p.canvas
	art := p.scene.Artwork
	selectionError := p.scene.SelectionError
	w, h := p.width, p.height
	sy := p.safeY
	bottom := p.bottom
	v := &p.scene.Content
	s := p.scene

	c.Image(art.Photo, 0, 0, w, h)
	if s.PhotoControlsVisible {
		c.Shade(0, 0, w, sy+12, 175)
		rows := controlRows(w, []controlHint{hint(s.Controls, control.Previous, "Previous"), hint(s.Controls, control.Next, "Next"), hint(s.Controls, control.Back, "Back")})
		top := controlsTop(bottom, rows) - 3
		c.Shade(0, top, w, h-top, 175)
		count := s.PhotoCount
		c.Text(24, sy, truncate(v.Detail.Name, w-60-textWidth(count, 1), 1), 0xffffff, w-24)
		c.Text(w-24-textWidth(count, 1), sy, count, dimColor, w-24)
		drawControls(c, bottom, rows)
	}

	if s.Notice != "" {
		c.Shade(0, h/2-10, w, 24, 210)
		center(c, h/2-4, s.Notice, 0xffffff, 1)
	}
	if art.Photo == nil {
		message := "Loading photo..."
		if selectionError != "" {
			message = "Photo unavailable. R:retry"
		}
		center(c, h/2-4, message, 0xffffff, 1)
	}
}
