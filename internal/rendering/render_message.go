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

func drawMessage(c *ui.Canvas, m MessagePresentation, now time.Time) {
	if m.Text == "" || !now.Before(m.Until) {
		return
	}
	width := c.Width - 64
	top := safeY(c.Width, c.Height) + 8
	height := 4*12 + 18
	if m.Header != "" {
		height += 16
	}
	c.Shade(32, top, width, height, 225)
	y := top + 8
	if m.Header != "" {
		center(c, y, truncate(m.Header, width-24, 1), titleColor, 1)
		y += 16
	}
	c.Wrap(44, y, width-24, 4, m.Text, 0xffffff)
}
