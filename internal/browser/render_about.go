package browser

import (
	"misterfin-crt/internal/branding"
	"misterfin-crt/internal/ui"
)

// about draws project identity and release state using the shared raster path.
// Controls use the same binding labels and safe margins as browsing screens.
func (p *screenPainter) about() {
	a := p.scene.About
	hints := []controlHint{hint(p.scene.Controls, "back", "Back")}
	if a.Release.Available && !a.Checking {
		hints = append(hints, hint(p.scene.Controls, "open", "Update"))
	}
	if !a.Checking {
		hints = append(hints, hint(p.scene.Controls, "select", "Check updates"))
	}
	rows := controlRows(p.width, hints)
	statusY := controlsTop(p.bottom, rows) - 18
	p.cache.about(p.canvas, statusY)
	center(p.canvas, statusY-62, truncate("Version "+a.Build.String(), p.width-48, 1), dimColor, 1)
	text := a.status(p.scene.Now)
	color := uint32(0xc0c0c0)
	if a.Release.Available && !a.Checking && a.Message == "" && !p.scene.Now.Before(a.UpdateNoticeUntil) {
		color = titleColor
	}
	center(p.canvas, statusY, truncate(text, p.width-48, 1), color, 1)
	drawControls(p.canvas, p.bottom, rows)
}

// about caches the static logo and attribution at the current geometry. Source
// artwork is embedded and decoded once. Repeated draws only copy prepared pixels.
func (s *sceneCache) about(c *ui.Canvas, statusY int) {
	draw := func(dst *ui.Canvas) {
		dst.Rect(0, 0, dst.Width, dst.Height, 0x0b0d13)
		top := safeY(dst.Width, dst.Height) + 4
		titleY := statusY - 86
		dst.Image(branding.Logo(), 24, top, dst.Width-48, max(1, titleY-top-8))
		center(dst, titleY, "MiSTerFin CRT", titleColor, 2)
		center(dst, statusY-44, "Based on MiSTerFin by Pudding Studio.", 0xc0c0c0, 1)
		center(dst, statusY-32, "Original © 2026 Pudding Studio. Changes © 2026 Trent Nix.", dimColor, 1)
		center(dst, statusY-20, "CC BY-NC 4.0. Components have separate licenses.", dimColor, 1)
	}
	if s == nil {
		draw(c)
		return
	}
	if s.aboutBase == nil || s.aboutBase.Width != c.Width || s.aboutBase.Height != c.Height || s.aboutStatusY != statusY {
		s.aboutBase = ui.New(c.Width, c.Height)
		s.aboutStatusY = statusY
		draw(s.aboutBase)
	}
	copy(c.Pixels, s.aboutBase.Pixels)
}
