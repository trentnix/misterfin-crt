package browser

import (
	"fmt"
	"math"

	"misterfin-go/internal/jellyfin"
	"misterfin-go/internal/ui"
)

// details draws metadata and returns the appropriate play or resume hint.
func (p *screenPainter) details() string {
	c := p.canvas
	cache := p.cache
	art := p.scene.Artwork
	w, h := p.width, p.height
	sy := p.safeY
	v := &p.scene.View

	hero := max(80, min(150, h-88))
	full := max(h*3/4, hero)
	cache.backdrop(c, art, true, func(layer *ui.Canvas) {
		layer.Rect(0, 0, w, full, 0x181818)
		layer.Blit(art.Backdrop, 0, 0, w, full)
		for y := 0; y < full; y++ {
			layer.Shade(0, y, w, 1, y*255/max(1, full-1))
		}
	})
	p.clock()
	cy := hero - 22 + 3
	if art.Logo != nil {
		c.Image(art.Logo, (w-480)/2, cy-22, 480, 44)
	} else {
		center(c, cy-4, truncate(itemTitle(*v.Detail), w-48, 1), 0xffffff, 1)
	}
	ty := max(hero+4, h-8-sy-34-50)
	metadataX := 24
	if v.Detail.ProductionYear > 0 {
		year := fmt.Sprint(v.Detail.ProductionYear)
		c.Text(metadataX, ty, year, dimColor, w)
		metadataX += textWidth(year, 1) + 8
	}
	if v.Detail.CommunityRating > 0 {
		for y := 0; y < 5; y++ {
			inset := int(math.Abs(float64(y - 2)))
			c.Rect(metadataX+inset, ty+1+y, 5-2*inset, 1, 0xffd700)
		}
		c.Text(metadataX+9, ty, fmt.Sprintf("%.1f", v.Detail.CommunityRating), dimColor, w)
	}
	s, col := subtitle(*v.Detail)
	if jellyfin.IsLive(*v.Detail) {
		center(c, ty, truncate(s, w-48, 1), col, 1)
	} else {
		c.Text(w-24-textWidth(s, 1), ty, s, col, w-24)
	}
	c.Wrap(24, ty+16, w-48, 3, v.Detail.Overview, 0xcccccc)
	hint := "B:play  A:back"
	if resumableVideo(v.Detail) {
		hint = "B:resume  SELECT:restart  A:back"
	}

	return hint
}
