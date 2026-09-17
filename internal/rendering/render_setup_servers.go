package rendering

import (
	"fmt"

	"mistervision/internal/ui"
)

// setupServers scrolls a bounded snapshot behind the selection. Each row shows
// both a server name and address so duplicate names remain distinguishable.
func setupServers(c *ui.Canvas, top, bottom int, s SetupPresentation) {
	const rowHeight = 26
	rows := max(1, (bottom-top-14)/rowHeight)
	selected := max(0, min(s.Selected, len(s.Servers)-1))
	start := max(0, min(selected-rows/2, len(s.Servers)-rows))
	for i := start; i < min(len(s.Servers), start+rows); i++ {
		y := top + (i-start)*rowHeight
		color := uint32(0xcccccc)
		if i == selected {
			c.Rect(24, y-3, c.Width-48, rowHeight-2, 0x283446)
			color = titleColor
		}
		c.Text(32, y, truncate(s.Servers[i].Name, c.Width-64, 1), color, c.Width-64)
		c.Text(32, y+12, truncate(s.Servers[i].URL, c.Width-64, 1), dimColor, c.Width-64)
	}
	if len(s.Servers) > 0 {
		center(c, bottom-8, fmt.Sprintf("%d / %d", selected+1, len(s.Servers)), dimColor, 1)
	}
}
