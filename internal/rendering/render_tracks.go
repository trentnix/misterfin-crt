package rendering

import (
	"misterfin-crt/internal/caption"

	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/ui"
)

// drawTrackMenu uses the shared overlay canvas on every video output.
func drawTrackMenu(c *ui.Canvas, menu *TrackMenu, labels control.Labels) {
	w, h := c.Width, c.Height
	sy := safeY(w, h)
	bottom := h - 8 - sy
	hints := []controlHint{pairedHint(labels, control.Previous, control.Next, "Tabs")}
	if len(menu.Rows) > 0 {
		hints = append(hints, hint(labels, control.Open, "Apply"))
	}
	hints = append(hints, hint(labels, control.Back, "Back"))
	if menu.Tab == 0 && menu.Delay != "" {
		hints = append(hints, hint(labels, control.SeekBackward, "Earlier"), hint(labels, control.SeekForward, "Later"))
	}
	controls := controlRows(w, hints)
	c.Shade(12, sy-4, w-24, h-2*sy+12, 225)
	tabY := sy + 4
	for i, title := range []string{"Subtitles", "Audio", "Picture"} {
		x := w*(2*i+1)/6 - textWidth(title, 1)/2
		color := uint32(dimColor)
		if menu.Tab == i {
			color = titleColor
			c.Rect(x-6, tabY+12, textWidth(title, 1)+12, 2, titleColor)
		}
		c.Text(x, tabY, title, color, w-24)
	}
	top := tabY + 24
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

// drawSubtitle positions a cached Unicode cue above playback controls.
func drawSubtitle(c *ui.Canvas, captions *caption.Renderer, text string, bottom int) {
	image := captions.Image(text, c.Width, c.Height)
	if image == nil {
		return
	}
	c.Overlay(image, (c.Width-image.Bounds().Dx())/2, bottom-image.Bounds().Dy())
}
