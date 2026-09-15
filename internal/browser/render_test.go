package browser

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
	"time"

	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/jellyfin"
)

func TestCRTLayoutAndExitOverlay(t *testing.T) {
	for _, h := range []int{240, 288} {
		m := New()
		m.ListMode = true
		m.Rows = visibleRows(640, h)
		m.Current().Page.Items = []jellyfin.Item{{Name: "Movie", Type: "Movie"}}
		pixels := render(640, h, m, SetupPresentation{}, Artwork{}, "", Animation{}, time.Date(2026, 1, 1, 12, 34, 0, 0, time.UTC))
		sy := safeY(640, h)
		if (h == 240 && (sy != 12 || m.Rows != 6)) || (h == 288 && (sy != 14 || m.Rows != 7)) {
			t.Fatalf("CRT geometry: height=%d margin=%d rows=%d", h, sy, m.Rows)
		}
		i := ((sy+21)*640 + 20) * 4
		if pixels[i] != 0x7c || pixels[i+1] != 0x37 || pixels[i+2] != 0x0d {
			t.Fatal("selection placement or palette changed")
		}
		m.Key(control.Back)
		dialog := render(640, h, m, SetupPresentation{}, Artwork{}, "", Animation{}, time.Time{})
		different := false
		for i := (h/2 - 20) * 640 * 4; i < (h/2+20)*640*4; i++ {
			if pixels[i] != dialog[i] {
				different = true
				break
			}
		}
		if !different {
			t.Fatal("missing exit confirmation")
		}
	}
}

func TestChannelRowsShowNumberAndGuide(t *testing.T) {
	item := jellyfin.Item{Name: "Local News", Type: "TvChannel", Number: "12.2", ChannelNumber: "99"}
	item.CurrentProgram.Name = "Evening News"
	if got := itemTitle(item); got != "12.2  Local News" {
		t.Fatal(got)
	}
	if got, _ := subtitle(item); got != "Evening News" {
		t.Fatal(got)
	}
	item.Number = ""
	item.CurrentProgram.Name = ""
	if got := itemTitle(item); got != "99  Local News" {
		t.Fatal(got)
	}
	if got, _ := subtitle(item); got != "No guide information" {
		t.Fatal(got)
	}
}

func TestCarouselShowsLibraryName(t *testing.T) {
	m := New()
	m.Current().Page.Items = []jellyfin.Item{{Name: "Family Cinema", CollectionType: "movies"}}
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	frame := render(640, 240, m, SetupPresentation{}, Artwork{}, "", Animation{}, now)
	m.Current().Page.Items[0].CollectionType = "tvshows"
	sameName := render(640, 240, m, SetupPresentation{}, Artwork{}, "", Animation{}, now)
	for i := range frame {
		if frame[i] != sameName[i] {
			t.Fatal("library type changed the displayed name")
		}
	}
	m.Current().Page.Items[0].Name = "Family Cinema 4K"
	otherName := render(640, 240, m, SetupPresentation{}, Artwork{}, "", Animation{}, now)
	for i := range frame {
		if frame[i] != otherName[i] {
			t.Fatal("carousel name exceeds the C client's 160-pixel limit")
		}
	}
}

func TestPhotoFitsPhysicalCRTAspect(t *testing.T) {
	for _, height := range []int{240, 288} {
		m := New()
		m.Stack = append(m.Stack, View{Detail: &jellyfin.Item{ID: "photo", Name: "Portrait", Type: "Photo"}})
		photo := image.NewRGBA(image.Rect(0, 0, 9, 16))
		draw.Draw(photo, photo.Bounds(), image.NewUniform(color.RGBA{R: 255, A: 255}), image.Point{}, draw.Src)
		frame := render(640, height, m, SetupPresentation{}, Artwork{Photo: photo}, "", Animation{}, time.Time{})
		red := func(x, y int) bool { offset := (y*640 + x) * 4; return frame[offset+2] == 255 && frame[offset] == 0 }
		// A 9:16 portrait fills the screen height and occupies 270 logical columns.
		if !red(320, height/2) || !red(190, height/2) || red(180, height/2) || red(460, height/2) {
			t.Fatal("photo aspect ratio or letterboxing changed")
		}
		if m.Key(control.Open) != nil || m.Notice != "" {
			t.Fatal("photo tried to start playback")
		}
		m.Key(control.Back)
		if len(m.Stack) != 1 {
			t.Fatal("photo did not return to parent")
		}
	}
}

func TestWaitAnimationTraversesEveryBlock(t *testing.T) {
	// This date overflows a 32-bit int if the timestamp is narrowed before modulo.
	start := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	for _, label := range []string{"Loading...", "Buffering...", "Seeking..."} {
		for _, height := range []int{240, 288} {
			for tick := 0; tick < 16; tick++ {
				now := start.Add(time.Duration(tick) * 150 * time.Millisecond)
				pixels := renderVideoOverlay(640, height, PlaybackPresentation{WaitLabel: label}, now)
				want := int((now.UnixMilli() / 150) % 8)
				for block := 0; block < 8; block++ {
					i := ((height/2+5)*640 + 640/2 - 46 + block*12) * 4
					got := uint32(pixels[i]) | uint32(pixels[i+1])<<8 | uint32(pixels[i+2])<<16
					color := uint32(0x505050)
					if block == want {
						color = titleColor
					}
					if got != color {
						t.Fatalf("%s height=%d tick=%d block=%d: got %x want %x", label, height, tick, block, got, color)
					}
				}
			}
		}
	}
}

func TestMetadataMatchesCBaseline(t *testing.T) {
	for _, tc := range []struct {
		item jellyfin.Item
		want string
	}{
		{jellyfin.Item{Type: "MusicArtist"}, ""},
		{jellyfin.Item{Type: "MusicArtist", ChildCount: 1}, "1 album"},
		{jellyfin.Item{Type: "MusicAlbum", ChildCount: 1}, "1 track"},
		{jellyfin.Item{Type: "MusicAlbum", ProductionYear: 2024}, "2024"},
		{jellyfin.Item{Type: "MusicAlbum", ProductionYear: 2024, ChildCount: 2}, "2024 - 2 tracks"},
		{jellyfin.Item{Type: "Series", ChildCount: 1, RecursiveItemCount: 1}, "1 season - 1 episode"},
		{jellyfin.Item{Type: "Series", ChildCount: 2}, "2 seasons"},
	} {
		if got, _ := subtitle(tc.item); got != tc.want {
			t.Fatalf("%+v: got %q want %q", tc.item, got, tc.want)
		}
	}
	for _, tc := range []struct {
		seconds int64
		want    string
	}{{-1, "0:00"}, {3599, "59:59"}, {3600, "1:00:00"}, {7384, "2:03:04"}} {
		if got := runtime(tc.seconds * 10000000); got != tc.want {
			t.Fatalf("%d: %s", tc.seconds, got)
		}
	}
}

func TestListBackdropFadesWithinWideImage(t *testing.T) {
	for _, h := range []int{240, 288} {
		m := New()
		m.ListMode = true
		source := image.NewRGBA(image.Rect(0, 0, 16, 9))
		draw.Draw(source, source.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		pixels := render(640, h, m, SetupPresentation{}, Artwork{Backdrop: source}, "", Animation{}, time.Time{})
		// Left edge avoids text and selection. The hero ends at three quarters height.
		if pixels[0] != 110 || pixels[(h*3/4-1)*640*4] != 0 || pixels[(h-1)*640*4] != 0 {
			t.Fatal("backdrop brightness or fade extent differs from C")
		}
	}
}

func TestHeaderMarqueePreservesSafeMargins(t *testing.T) {
	m := New()
	m.Stack = append(m.Stack, View{Title: "A very long library title that must scroll without covering the clock"})
	start := render(640, 240, m, SetupPresentation{}, Artwork{}, "", Animation{}, time.Time{})
	moved := render(640, 240, m, SetupPresentation{}, Artwork{}, "", Animation{TitleSeconds: 2}, time.Time{})
	if string(start) == string(moved) {
		t.Fatal("long header did not scroll")
	}
	for y := safeY(640, 240); y < safeY(640, 240)+16; y++ {
		for x := 0; x < 640; x++ {
			if x >= 24 && x < 556 {
				continue
			}
			i := (y*640 + x) * 4
			if string(start[i:i+4]) != string(moved[i:i+4]) {
				t.Fatal("marquee changed pixels outside title area")
			}
		}
	}
}

func TestSeekUsesOnlyTheOpenMenu(t *testing.T) {
	for _, height := range []int{240, 288} {
		for _, wait := range []string{"", "Seeking...", "Loading..."} {
			p := PlaybackPresentation{Title: "Episode", ControlsVisible: true, Seekable: true,
				HasDestination: true, ShowDestination: wait == "", DestinationTicks: 900000000, WaitLabel: wait}
			pixels := renderVideoOverlay(640, height, p, time.Unix(100, 0))
			menuTop := height - 8 - safeY(640, height) - 46
			for i := 3; i < menuTop*640*4; i += 4 {
				if pixels[i] != 0 {
					t.Fatalf("%s: seek feedback escaped the open menu", wait)
				}
			}
			if pixels[(menuTop*640)*4+3] == 0 {
				t.Fatal("seek hid the playback menu")
			}
			p.ControlsVisible = false
			pixels = renderVideoOverlay(640, height, p, time.Unix(100, 0))
			if pixels[((height/2)*640+320)*4+3] == 0 {
				t.Fatal("hidden-menu seek lost the centered feedback")
			}
		}
	}
}
