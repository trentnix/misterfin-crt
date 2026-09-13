package browser

import (
	"strings"

	"misterfin-go/internal/input/control"
	"misterfin-go/internal/ui"
)

// drawTrackMenu uses the shared overlay canvas on every video output.
func drawTrackMenu(c *ui.Canvas, menu *TrackMenu, labels control.Labels) {
	w, h := c.Width, c.Height
	sy := safeY(w, h)
	bottom := h - 8 - sy
	hints := []controlHint{pairedHint(labels, "previous", "next", "Tabs"), hint(labels, "open", "Apply"), hint(labels, "back", "Back")}
	if menu.Tab == 0 && menu.Delay != "" {
		hints = append(hints, hint(labels, "seek-backward", "Earlier"), hint(labels, "seek-forward", "Later"))
	}
	controls := controlRows(w, hints)
	c.Shade(12, sy-4, w-24, h-2*sy+12, 225)
	for i, title := range []string{"Subtitles", "Audio"} {
		x := w/4 - textWidth(title, 1)/2 + i*w/2
		color := uint32(dimColor)
		if menu.Tab == i {
			color = titleColor
			c.Rect(x-6, sy+12, textWidth(title, 1)+12, 2, titleColor)
		}
		c.Text(x, sy, title, color, w-24)
	}
	top := sy + 24
	footer := controlsTop(bottom, controls) - 6
	if menu.Delay != "" {
		footer -= 12
		center(c, footer, menu.Delay, dimColor, 1)
	}
	if menu.Message != "" {
		footer -= 12
		center(c, footer, truncate(menu.Message, w-48, 1), titleColor, 1)
	}
	rows := max(1, (footer-top)/18)
	start := max(0, min(menu.Selected-rows/2, len(menu.Rows)-rows))
	for i := start; i < min(len(menu.Rows), start+rows); i++ {
		y := top + (i-start)*18
		row := menu.Rows[i]
		if i == menu.Selected {
			c.Rect(20, y-3, w-40, 16, 0x0d377c)
		}
		prefix := "  "
		if row.Active {
			prefix = "* "
		}
		c.Text(26, y, prefix+truncate(row.Label, w-84, 1), 0xffffff, w-32)
	}
	if start > 0 {
		c.Text(w-32, top, "^", titleColor, w-16)
	}
	if start+rows < len(menu.Rows) {
		c.Text(w-32, top+(rows-1)*18, "v", titleColor, w-16)
	}
	drawControls(c, bottom, controls)
}

// drawSubtitle paints readable text above playback controls, with an outline.
// Layout is bounded to three lines so malformed cues cannot cover the screen.
func drawSubtitle(c *ui.Canvas, text string, bottom int) {
	if text == "" {
		return
	}
	width := c.Width - 64
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		line := ""
		for _, word := range strings.Fields(paragraph) {
			if textWidth(line+" "+word, 1) > width && line != "" {
				lines = append(lines, line)
				line = ""
			}
			if line != "" {
				line += " "
			}
			line += truncate(word, width, 1)
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	lines = lines[:min(3, len(lines))]
	top := bottom - len(lines)*12
	for i, line := range lines {
		x := (c.Width - textWidth(line, 1)) / 2
		y := top + i*12
		c.Shade(x-4, y-2, textWidth(line, 1)+8, 12, 100)
		for _, d := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			c.Text(x+d[0], y+d[1], line, 0, c.Width-24)
		}
		c.Text(x, y, line, 0xffffff, c.Width-24)
	}
}
