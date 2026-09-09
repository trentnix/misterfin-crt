package browser

import (
	"fmt"
	"image"
	"misterfin-go/internal/ui"
)

func Render(w, h int, m *Model, status string, art image.Image, artError string) []byte {
	c := ui.New(w, h)
	c.Rect(0, 0, w, h, 0x101923)
	c.Rect(0, 0, w, 27, 0x1d3042)
	c.Text(14, 10, "MiSTerFin-Go", 0x65dfc7, w)
	if status != "" {
		c.Wrap(22, 55, w-44, 10, status, 0xe7edf3)
		c.Text(14, h-18, "R: retry    Q: quit", 0x93adbf, w)
		return c.Pixels
	}
	v := m.Current()
	c.Text(14, 37, v.Title, 0xffffff, w-15)
	if v.Detail != nil {
		item := v.Detail
		c.Image(art, 18, 61, w/3, h-100)
		c.Wrap(w/3+35, 65, w*2/3-50, 4, item.Name, 0xffffff)
		c.Text(w/3+35, 120, fmt.Sprintf("%s  %d", item.Type, item.ProductionYear), 0x93adbf, w-15)
		c.Wrap(w/3+35, 145, w*2/3-50, 3, "Browsing prototype. Playback is not available yet.", 0x65dfc7)
	} else {
		rows := max(1, (h-98)/15)
		top := v.Selected / rows * rows
		listWidth := w * 2 / 3
		for row := 0; row < rows && top+row < len(v.Page.Items); row++ {
			i := top + row
			y := 60 + row*15
			item := v.Page.Items[i]
			color := uint32(0xbacbd7)
			if i == v.Selected {
				c.Rect(8, y-3, listWidth-12, 14, 0x2a5364)
				color = 0xffffff
			}
			name := item.Name
			if item.UserData.Played {
				name = "* " + name
			} else if item.UserData.PlaybackPositionTicks > 0 {
				name = "> " + name
			}
			c.Text(15, y, name, color, listWidth-12)
		}
		c.Rect(listWidth, 59, w-listWidth-14, h-98, 0x182936)
		if art != nil {
			c.Image(art, listWidth+8, 65, w-listWidth-30, h-125)
		} else {
			c.Text(listWidth+15, 85, "No artwork", 0x93adbf, w-15)
		}
		if item := v.Item(); item != nil {
			c.Text(listWidth+8, h-51, item.Type, 0x93adbf, w-15)
		}
		c.Text(w-130, 10, v.Count(), 0x93adbf, w-10)
		if len(v.Page.Items) == 0 && !v.Loading && v.Error == "" {
			c.Text(15, 67, "Nothing here", 0x93adbf, listWidth)
		}
	}
	message := artError
	if v.Loading {
		message = "Loading...  A: cancel"
	}
	if v.Error != "" {
		message = v.Error + "  R: retry"
	}
	if message != "" {
		c.Rect(0, h-37, w, 15, 0x263542)
		c.Text(14, h-34, message, 0xffd183, w-12)
	}
	c.Text(14, h-16, "B/Enter: open  A/Esc: back  Left/Right: page  Q: quit", 0x93adbf, w-10)
	return c.Pixels
}
