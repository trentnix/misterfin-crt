package browser

import (
	"math"

	"misterfin-go/internal/ui"
)

// list draws a paginated item list and returns its navigation hint.
func (p *screenPainter) list() string {
	c := p.canvas
	cache := p.cache
	art := p.scene.Artwork
	w, h := p.width, p.height
	sy := p.safeY
	bottom := p.bottom
	anim := p.animation
	v := &p.scene.View
	s := p.scene
	hint := "B:select  A:back"
	if canShuffle(*v) {
		label := s.Controls.Name("select")
		if label != "" {
			hint = "B:select  " + label + ":shuffle library  A:back"
		}
	}

	cache.backdrop(c, art, false, func(layer *ui.Canvas) {
		if art.Backdrop != nil {
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
	title := v.Title
	if s.Root {
		title = "MiSTerFin-Go"
		hint = "B:select  SELECT:cover view  A:exit"
	}
	p.header(title)
	width := w - 48
	if art.Primary != nil {
		width = w - 24 - 175 - 10 - 24
	}
	// Borrow a vertical slice of the frame to clip moving rows without an
	// intermediate image or a full-frame copy. Header and footer stay fixed.
	top := sy + 21
	rows := visibleRows(w, h)
	list := *c
	list.Height = min(rows*30, h-top)
	list.Pixels = c.Pixels[top*w*4 : (top+list.Height)*w*4]
	if len(v.Page.Items) > 0 {
		list.Rect(20, int(math.Round(anim.Row*30)), width+8, 28, 0x0d377c)
	}
	scroll := float64(v.Scroll) + anim.ScrollOffset
	for index := max(0, int(math.Floor(scroll))); index < min(len(v.Page.Items), int(math.Ceil(scroll))+rows); index++ {
		item := v.Page.Items[index]
		y := 3 + int(math.Round((float64(index)-scroll)*30))
		color := uint32(0xcccccc)
		if index == v.Selected {
			color = 0xffffff
		}
		list.Text(24, y, truncate(itemTitle(item), width, 1), color, 24+width)
		s, col := subtitle(item)
		if v.Location.Kind == "continue" {
			s, col = continueSubtitle(item), titleColor
		}
		list.Text(24, y+11, truncate(s, width, 1), col, 24+width)
	}
	if len(v.Page.Items) == 0 && !v.Loading {
		center(c, h/2, "Nothing here", dimColor, 1)
	}
	if v.Page.TotalRecordCount != nil && *v.Page.TotalRecordCount > visibleRows(w, h) {
		s := v.Count()
		c.Text(w-24-textWidth(s, 1), bottom, s, dimColor, w-24)
	}

	return hint
}
