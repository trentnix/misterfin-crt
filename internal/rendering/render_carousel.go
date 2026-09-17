package rendering

import (
	"fmt"

	"mistervision/internal/input/control"
)

// carousel draws library names over cached artwork strips and returns its controls.
func (p *screenPainter) carousel() [][]controlHint {
	c := p.canvas
	cache := p.cache
	art := p.scene.Artwork
	w, h := p.width, p.height
	sy := p.safeY
	anim := p.animation
	v := &p.scene.Content

	if p.scene.Background != nil {
		cache.customBackground(c, p.scene.Background)
	} else if len(art.Covers) > 0 {
		music := v.Item() != nil && v.Item().CollectionType == "music"
		cache.mosaic(c, art.Covers, music, anim.Seconds)
	}
	p.header(p.scene.title(), sy+4)
	if p.scene.About.Release.Available {
		c.Text(24, sy+24, "Update available", titleColor, w-24)
	}
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
			if i == v.Selected && p.scene.LibraryLoading {
				c.Text(w/2-textWidth("Loading...", 1)/2, cy+12, "Loading...", dimColor, w)
			} else if i == v.Selected && p.scene.LibraryCount != nil {
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
				count := fmt.Sprintf("%d %s", *p.scene.LibraryCount, label)
				c.Text(w/2-textWidth(count, 1)/2, cy+12, count, dimColor, w)
			}
		}
	}
	labels := p.scene.Controls
	hints := []controlHint{pairedHint(labels, control.Previous, control.Next, "Browse")}
	if v.Item() != nil {
		hints = append(hints, hint(labels, control.Open, "Select"))
	}
	hints = append(hints, hint(labels, control.Select, "List"), hint(labels, control.Back, "Exit"), hint(labels, control.About, "About"))
	if v.Error != "" || p.scene.SelectionError != "" {
		hints = append(hints, hint(labels, control.Retry, "Retry"))
	}
	return controlRows(w, hints)
}
