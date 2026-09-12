package browser

import (
	"fmt"
	"image"
	"math"
	"misterfin-go/internal/jellyfin"
	"misterfin-go/internal/ui"
	"strings"
	"time"
)

const titleColor = 0xffe040
const dimColor = 0x808080

func safeY(w, h int) int                { return int(24*float64(h*4)/float64(w*3) + 0.5) }
func visibleRows(w, h int) int          { return max(1, (h-2*safeY(w, h)-32)/30) }
func textWidth(s string, scale int) int { return len([]rune(s)) * 8 * scale }
func truncate(s string, width, scale int) string {
	r := []rune(s)
	n := max(0, width/(8*scale))
	if len(r) <= n {
		return s
	}
	if n > 3 {
		return string(r[:n-3]) + "..."
	}
	return string(r[:n])
}
func center(c *ui.Canvas, y int, s string, color uint32, scale int) {
	c.TextScaled((c.Width-textWidth(s, scale))/2, y, s, color, c.Width, scale)
}
func runtime(ticks int64) string {
	seconds := max(int64(0), ticks/10000000)
	if seconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
	}
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}
func subtitle(i jellyfin.Item) (string, uint32) {
	color := uint32(0x585858)
	switch i.Type {
	case "TvChannel", "LiveTvChannel":
		if i.CurrentProgram.Name != "" {
			return i.CurrentProgram.Name, color
		}
		return "No guide information", color
	case "MusicArtist":
		return positiveCount(i.ChildCount, "album"), color
	case "MusicAlbum":
		parts := []string{}
		if i.ProductionYear > 0 {
			parts = append(parts, fmt.Sprint(i.ProductionYear))
		}
		if tracks := positiveCount(i.ChildCount, "track"); tracks != "" {
			parts = append(parts, tracks)
		}
		return strings.Join(parts, " - "), color
	case "Series":
		seasons := positiveCount(i.ChildCount, "season")
		if seasons != "" && i.RecursiveItemCount > 0 {
			seasons += " - " + positiveCount(i.RecursiveItemCount, "episode")
		}
		return seasons, color
	case "Audio":
		return runtime(i.RunTimeTicks), color
	}
	s := ""
	if i.RunTimeTicks > 0 {
		s = fmt.Sprintf("%d min", i.RunTimeTicks/600000000)
	}
	if i.UserData.Played {
		s += " - watched"
		color = 0x40cc40
	} else if i.UserData.PlaybackPositionTicks > 0 {
		s += " - resume " + runtime(i.UserData.PlaybackPositionTicks)
		color = 0xffc040
	}
	return strings.TrimPrefix(s, " - "), color
}
func itemTitle(i jellyfin.Item) string {
	s := i.Name
	if jellyfin.IsLive(i) {
		number := i.Number
		if number == "" {
			number = i.ChannelNumber
		}
		if number != "" {
			return number + "  " + s
		}
		return s
	}
	if i.IsFolder || i.Type == "Series" || i.Type == "Season" || i.Type == "MusicArtist" || i.Type == "MusicAlbum" {
		s = "> " + s
	}
	if i.ProductionYear > 0 && (i.Type == "Movie" || i.Type == "MusicVideo" || i.Type == "Video" || i.Type == "Series") {
		s += fmt.Sprintf(" (%d)", i.ProductionYear)
	}
	return s
}

type Artwork struct {
	Primary, Backdrop, Logo, Photo image.Image
	Covers                         []image.Image
	Count                          *int
}
type Animation struct{ Seconds, TitleSeconds, Selection, Row float64 }

func Render(w, h int, m *Model, status string, art image.Image, artError string) []byte {
	return render(w, h, m, status, Artwork{Primary: art}, artError, Animation{Selection: float64(m.Current().Selected), Row: float64(m.Current().Selected - m.Current().Scroll)}, time.Now())
}
func render(w, h int, m *Model, status string, art Artwork, artError string, anim Animation, now time.Time) []byte {
	return renderScene(ui.New(w, h), nil, sceneFromModel(m, status, art, artError, now), anim)
}

func renderScene(c *ui.Canvas, cache *sceneCache, s Scene, anim Animation) []byte {
	status, art, artError, now := s.Status, s.Artwork, s.ArtworkError, s.Now
	w, h := c.Width, c.Height
	sy := safeY(w, h)
	bottom := h - 8 - sy
	v := &s.View
	clock := func() { c.Text(w-72, sy+4, now.Format("15:04"), dimColor, w-32) }
	header := func(title string) {
		end := w - 84
		x := 24
		if textWidth(title, 2) > end-x {
			x -= int(anim.TitleSeconds*15) % (textWidth(title, 2) + 40)
		}
		// Clip the marquee to the title's safe area, including its repeated copy.
		layer := ui.New(w, 16)
		layer.TextScaled(x, 0, title, titleColor, end, 2)
		if x < 24 {
			layer.TextScaled(x+textWidth(title, 2)+40, 0, title, titleColor, end, 2)
		}
		for y := sy; y < min(h, sy+16); y++ {
			for x := 24; x < end; x++ {
				i := (y*w + x) * 4
				j := ((y-sy)*w + x) * 4
				if layer.Pixels[j]|layer.Pixels[j+1]|layer.Pixels[j+2] != 0 {
					copy(c.Pixels[i:i+3], layer.Pixels[j:j+3])
				}
			}
		}
		clock()
	}
	if status != "" {
		heading := "MiSTerFin-Go"
		if strings.HasPrefix(status, "Quick Connect: ") {
			heading = "Quick Connect"
		} else if status != "Connecting to Jellyfin..." {
			heading = "Can't connect to server"
		}
		center(c, h/2-44, heading, titleColor, 2)
		if strings.HasPrefix(status, "Quick Connect: ") {
			code := strings.Split(strings.TrimPrefix(status, "Quick Connect: "), "\n")[0]
			center(c, h/2-12, "Enter this code in your Jellyfin client:", dimColor, 1)
			center(c, h/2+8, code, 0xffffff, 3)
			center(c, h/2+48, "Waiting for sign-in...", 0xcccccc, 1)
		} else {
			c.Wrap(24, h/2, w-48, 6, status, 0xcccccc)
		}
		center(c, bottom, "B:try again   A:exit", dimColor, 1)
		return c.Pixels
	}
	if v.Detail != nil && v.Detail.Type == "Photo" {
		c.Image(art.Photo, 0, 0, w, h)
		if s.Playback.ControlsVisible {
			c.Shade(0, 0, w, sy+12, 175)
			c.Shade(0, bottom-4, w, h-bottom+4, 175)
			count := s.PhotoCount
			c.Text(24, sy, truncate(v.Detail.Name, w-60-textWidth(count, 1), 1), 0xffffff, w-24)
			c.Text(w-24-textWidth(count, 1), sy, count, dimColor, w-24)
			center(c, bottom, "LEFT/RIGHT: photos   A:back", dimColor, 1)
		}

		if s.Notice != "" {
			c.Shade(0, h/2-10, w, 24, 210)
			center(c, h/2-4, s.Notice, 0xffffff, 1)
		}
		if art.Photo == nil {
			message := "Loading photo..."
			if artError != "" {
				message = "Photo unavailable. R:retry"
			}
			center(c, h/2-4, message, 0xffffff, 1)
		}
		return c.Pixels
	}
	if s.Video && v.Detail != nil {
		cache.videoBackdrop(c, art.Backdrop)
		center(c, h/2-4, truncate(v.Detail.Name, w-48, 1), titleColor, 1)
		return c.Pixels
	}
	if s.Audio && v.Detail != nil {
		header("Now playing")
		c.Image(art.Primary, 24, sy+28, w-48, h-sy-98)
		center(c, h-sy-62, truncate(v.Detail.Name, w-48, 1), titleColor, 1)
		center(c, h-sy-46, runtime(s.Playback.PositionTicks)+" / "+runtime(v.Detail.RunTimeTicks), dimColor, 1)
		c.Rect(24, h-sy-30, w-48, 3, 0x303030)
		if v.Detail.RunTimeTicks > 0 {
			c.Rect(24, h-sy-30, int(min(s.Playback.PositionTicks, v.Detail.RunTimeTicks)*int64(w-48)/v.Detail.RunTimeTicks), 3, titleColor)
		}
		if s.Playback.ControlsVisible {
			c.Shade(0, bottom-22, w, h-bottom+22, 210)
			action := "B:pause"
			if s.Playback.Paused {
				action = "B:play"
			}
			center(c, bottom-12, "LEFT/RIGHT: previous/next track", dimColor, 1)
			center(c, bottom, action+"   A:stop", dimColor, 1)
		}
		if s.Notice != "" {
			center(c, bottom, s.Notice, dimColor, 1)
		}
		return c.Pixels
	}
	hint := "B:select  A:back"
	if v.Detail != nil {
		hero := max(80, min(150, h-88))
		full := max(h*3/4, hero)
		cache.backdrop(c, art, true, func(layer *ui.Canvas) {
			layer.Rect(0, 0, w, full, 0x181818)
			layer.Blit(art.Backdrop, 0, 0, w, full)
			for y := 0; y < full; y++ {
				layer.Shade(0, y, w, 1, y*255/max(1, full-1))
			}
		})
		clock()
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
		hint = "B:play  A:back"
		if resumableVideo(v.Detail) {
			hint = "B:resume  SELECT:restart  A:back"
		}
	} else if s.Root && !s.ListMode {
		if len(art.Covers) > 0 {
			music := v.Item() != nil && v.Item().CollectionType == "music"
			cache.mosaic(c, art.Covers, music, anim.Seconds)
		}
		header("MiSTerFin-Go")
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
		hint = "LEFT/RIGHT: browse   B:select   SELECT:list view   A:exit"
	} else {
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
		header(title)
		width := w - 48
		if art.Primary != nil {
			width = w - 24 - 175 - 10 - 24
		}
		if len(v.Page.Items) > 0 {
			c.Rect(20, sy+21+int(math.Round(anim.Row*30)), width+8, 28, 0x0d377c)
		}
		for row := 0; row < visibleRows(w, h) && v.Scroll+row < len(v.Page.Items); row++ {
			index := v.Scroll + row
			item := v.Page.Items[index]
			y := sy + 24 + row*30
			color := uint32(0xcccccc)
			if index == v.Selected {
				color = 0xffffff
			}
			c.Text(24, y, truncate(itemTitle(item), width, 1), color, 24+width)
			s, col := subtitle(item)
			c.Text(24, y+11, truncate(s, width, 1), col, 24+width)
		}
		if len(v.Page.Items) == 0 && !v.Loading {
			center(c, h/2, "Nothing here", dimColor, 1)
		}
		if v.Page.TotalRecordCount != nil && *v.Page.TotalRecordCount > visibleRows(w, h) {
			s := v.Count()
			c.Text(w-24-textWidth(s, 1), bottom, s, dimColor, w-24)
		}
	}
	center(c, bottom, hint, dimColor, 1)
	message := artError
	if v.Loading {
		message = "Loading..."
	}
	if v.Error != "" {
		message = v.Error + "  R:retry"
	}
	if message != "" {
		center(c, bottom-14, truncate(message, w-48, 1), 0xff6060, 1)
	}
	if s.ExitConfirm || s.Notice != "" {
		message := s.Notice
		scale := 1
		if s.ExitConfirm {
			message = "Exit? [B: yes  A: no]"
			scale = 2
		}
		width := textWidth(message, scale)
		c.Shade((w-width)/2-12, h/2-8*scale-12, width+24, 16*scale+24, 210)
		center(c, h/2-4*scale, message, titleColor, scale)
	}
	return c.Pixels
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
		action = "LEFT/RIGHT:30s   " + action
	}
	center(c, bottom, action+"   A:stop", dimColor, 1)
	return c.Pixels
}

// Compose controls over a fresh decoder frame. Hidden controls leave it intact.
func renderVideoControls(frame []byte, w, h int, m *Model, now time.Time) {
	ui.Composite(frame, renderVideoOverlay(w, h, m.PlaybackState.presentation(m.Current().Detail, now), now))
}

// positiveCount follows the C list metadata: omit unknown counts and pluralize.
func positiveCount(count int, name string) string {
	if count <= 0 {
		return ""
	}
	if count != 1 {
		name += "s"
	}
	return fmt.Sprintf("%d %s", count, name)
}
