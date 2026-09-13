package browser

import (
	"time"

	"misterfin-go/internal/ui"
)

// videoBackdrop draws the cached companion UI while an external player owns video.
func (p *screenPainter) videoBackdrop() {
	c := p.canvas
	cache := p.cache
	art := p.scene.Artwork
	w, h := p.width, p.height
	v := &p.scene.View

	cache.videoBackdrop(c, art.Backdrop)
	center(c, h/2-4, truncate(v.Detail.Name, w-48, 1), titleColor, 1)
}

// renderVideoOverlay returns straight-alpha BGRA pixels independent of the
// decoder and display that will present them.
func renderVideoOverlay(w, h int, p PlaybackPresentation, now time.Time) []byte {
	return renderVideoOverlayOn(ui.NewOverlay(w, h), p, now)
}

func renderVideoOverlayOn(c *ui.Canvas, p PlaybackPresentation, now time.Time) []byte {
	w, h := c.Width, c.Height
	seeking := p.ShowDestination
	if label := p.WaitLabel; !p.ControlsVisible && (label != "" || seeking) {
		// Match the display's 4:3 shape after logical CRT pixels are stretched.
		boxWidth := 140
		boxHeight := (boxWidth*h + w/2) / w
		c.Shade((w-boxWidth)/2, (h-boxHeight)/2, boxWidth, boxHeight, 64)
		if seeking {
			center(c, h/2-12, "Seek to", titleColor, 1)
			center(c, h/2+5, runtime(p.DestinationTicks), titleColor, 1)
			return c.Pixels
		}
		center(c, h/2-12, label, titleColor, 1)
		// Reduce in int64 before narrowing: MiSTer uses a 32-bit int.
		step := int((now.UnixMilli() / 150) % 8)
		for i := 0; i < 8; i++ {
			color := uint32(0x505050)
			if i == step {
				color = titleColor
			}
			c.Rect(w/2-46+i*12, h/2+5, 8, 4, color)
		}
	}
	if !p.ControlsVisible {
		return c.Pixels
	}
	sy := safeY(w, h)
	bottom := h - 8 - sy
	c.Shade(0, bottom-46, w, h-bottom+46, 210)
	center(c, bottom-36, truncate(p.Title, w-48, 1), titleColor, 1)
	label := runtime(p.PositionTicks)
	if p.HasDestination {
		label = "Seek to " + runtime(p.DestinationTicks)
	}
	if p.WaitLabel != "" {
		position := p.PositionTicks
		if p.HasDestination {
			position = p.DestinationTicks
		}
		label = p.WaitLabel + " " + runtime(position)
	}
	if p.Seekable && p.DurationTicks > 0 {
		label += " / " + runtime(p.DurationTicks)
	}
	center(c, bottom-22, label, dimColor, 1)
	action := "B:pause"
	if p.Paused {
		action = "B:play"
	}
	if p.Seekable {
		action = "LT/RT or J/L:30s   " + action
	}
	center(c, bottom, action+"   A:stop", dimColor, 1)
	return c.Pixels
}
