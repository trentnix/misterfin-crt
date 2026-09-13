package browser

import (
	"fmt"
)

// carousel draws library names over cached artwork strips and returns its controls.
func (p *screenPainter) carousel() string {
	c := p.canvas
	cache := p.cache
	art := p.scene.Artwork
	w, h := p.width, p.height
	sy := p.safeY
	anim := p.animation
	v := &p.scene.View

	if len(art.Covers) > 0 {
		music := v.Item() != nil && v.Item().CollectionType == "music"
		cache.mosaic(c, art.Covers, music, anim.Seconds)
	}
	p.header("MiSTerFin-Go")
	centers := make([]float64, len(v.Page.Items))
	names := make([]string, len(centers))
	for i, item := range v.Page.Items {
		names[i] = truncate(item.Name, 160, 2)
		if i > 0 {
			centers[i] = centers[i-1] + float64(textWidth(names[i-1], 2)+textWidth(names[i], 2))/2 + 74
		}
	}
	if len(centers) > 0 {
		pos := max(0, min(float64(len(centers)-1), anim.Selection))
		lo := int(pos)
		hi := min(lo+1, len(centers)-1)
		origin := centers[lo] + (centers[hi]-centers[lo])*(pos-float64(lo))
		cy := (sy + 24 + h - sy - 28) / 2
		for i, name := range names {
			x := w/2 + int(centers[i]-origin) - textWidth(name, 2)/2
			color := uint32(0xffffff)
			if i == v.Selected {
				color = titleColor
			}
			c.TextScaled(x, cy-10, name, color, w, 2)
			if i == v.Selected && art.Count != nil {
				label := "items"
				switch v.Page.Items[i].CollectionType {
				case "movies":
					label = "movies"
				case "tvshows":
					label = "series"
				case "music":
					label = "albums"
				case "musicvideos":
					label = "videos"
				}
				count := fmt.Sprintf("%d %s", *art.Count, label)
				c.Text(w/2-textWidth(count, 1)/2, cy+12, count, dimColor, w)
			}
		}
	}
	hint := "LEFT/RIGHT: browse   B:select   SELECT:list view   A:exit"

	return hint
}
