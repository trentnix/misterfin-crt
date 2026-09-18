package rendering

import (
	"time"

	"mistervision/internal/ui"
)

// MessagePresentation is a bounded, temporary banner shared by UI and video.
type MessagePresentation struct {
	Header, Text string
	Until        time.Time
}

// messageLines bounds wrapped text and marks any omitted tail explicitly.
func messageLines(text string, width, limit int) []string {
	lines := ui.WrapText(text, width)
	if len(lines) > limit {
		lines = lines[:limit]
		lines[limit-1] = truncate(lines[limit-1], max(8, width-24), 1) + "..."
	}
	return lines
}

func drawMessage(c *ui.Canvas, m MessagePresentation, now time.Time) {
	if m.Text == "" || !now.Before(m.Until) {
		return
	}
	top := safeY(c.Width, c.Height) + 8
	drawNotice(c, m.Header, m.Text, top, 6)
}

// drawNotice renders shared error guidance inside CRT-safe horizontal margins.
// Negative top centers the box. The caller reserves space for its controls.
func drawNotice(c *ui.Canvas, header, text string, top, limit int) {
	lines := messageLines(text, c.Width-88, limit)
	if len(lines) == 0 {
		return
	}
	width := textWidth(header, 1)
	for _, line := range lines {
		width = max(width, textWidth(line, 1))
	}
	width = min(c.Width-64, width+24)
	height := len(lines)*10 + 24
	if header != "" {
		height += 16
	}
	if top < 0 {
		top = (c.Height - height) / 2
	}
	c.Shade((c.Width-width)/2, top, width, height, 210)
	y := top + 12
	if header != "" {
		center(c, y, truncate(header, width-24, 1), titleColor, 1)
		y += 16
	}
	for _, line := range lines {
		center(c, y, line, titleColor, 1)
		y += 10
	}
}
