package rendering

import (
	"math"

	"mistervision/internal/input/control"
	"mistervision/internal/ui"
)

// list draws a paginated item list and returns its configured button badges.
func (p *screenPainter) list() [][]controlHint {
	c := p.canvas
	cache := p.cache
	art := p.scene.Artwork
	w, h := p.width, p.height
	sy := p.safeY
	anim := p.animation
	v := &p.scene.Content
	s := p.scene
	var hints []controlHint
	if v.Item() != nil {
		hints = append(hints, hint(s.Controls, control.Open, "Select"))
	}
	if v.CanShuffle {
		hints = append(hints, hint(s.Controls, control.Select, "Shuffle all"))
	}
	back := "Back"
	if s.Root {
		hints = append(hints, hint(s.Controls, control.Select, "Carousel"))
		back = "Exit"
	}
	if v.Error != "" || s.SelectionError != "" {
		hints = append(hints, hint(s.Controls, control.Retry, "Retry"))
	}
	hints = append(hints, hint(s.Controls, control.Back, back))
	controls := controlRows(w, hints)

	cache.backdrop(c, art, false, s.Background, func(layer *ui.Canvas) {
		if s.Background != nil {
			cache.customBackground(layer, s.Background)
		} else if art.Backdrop != nil {
			heroHeight := h * 3 / 4
			layer.Blit(art.Backdrop, 0, 0, w, heroHeight)
			for y := 0; y < heroHeight; y++ {
				brightness := 110 * (255 - y*255/max(1, heroHeight-1)) / 255
				layer.Shade(0, y, w, 1, 255-brightness)
			}
		}
		if art.Primary != nil {
			b := art.Primary.Bounds()
			par := float64(w*3) / float64(h*4)
			dh := min(140, int(175*float64(b.Dy())/float64(b.Dx())/par))
			layer.Image(art.Primary, w-24-175, sy+21, 175, dh)
		}
	})
	p.header(s.title(), p.safeY)
	width := w - 48
	if art.Primary != nil {
		width = w - 24 - 175 - 10 - 24
	}
	// Borrow a vertical slice of the frame to clip moving rows without an
	// intermediate image or a full-frame copy. Header and footer stay fixed.
	top := sy + 21
	rows := VisibleRows(w, h)
	// Keep the same logical rows and scroll position when custom labels wrap.
	// Compact their spacing only when the footer needs another instruction row.
	rowHeight := max(20, min(30, (controlsTop(p.bottom, controls)-16-top)/rows))
	list := *c
	list.Height = min(rows*rowHeight, h-top)
	list.Pixels = c.Pixels[top*w*4 : (top+list.Height)*w*4]
	if len(v.Page.Items) > 0 {
		list.Rect(20, int(math.Round(anim.Row*float64(rowHeight))), width+8, rowHeight-2, 0x0d377c)
	}
	scroll := float64(v.Scroll) + anim.ScrollOffset
	for index := max(0, int(math.Floor(scroll))); index < min(len(v.Page.Items), int(math.Ceil(scroll))+rows); index++ {
		item := v.Page.Items[index]
		y := 3 + int(math.Round((float64(index)-scroll)*float64(rowHeight)))
		color := uint32(0xcccccc)
		if index == v.Selected {
			color = 0xffffff
		}
		list.Text(24, y, truncate(itemTitle(item), width, 1), color, 24+width)
		s, col := subtitle(item)
		if v.Continue {
			s, col = continueSubtitle(item), titleColor
		}
		list.Text(24, y+11, truncate(s, width, 1), col, 24+width)
	}
	if len(v.Page.Items) == 0 && !v.Loading && v.Error == "" {
		center(c, h/2, "Nothing here", dimColor, 1)
	}
	return controls
}
