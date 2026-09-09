package browser

import (
	"misterfin-go/internal/jellyfin"
	"testing"
	"time"
)

func TestCRTLayoutAndExitOverlay(t *testing.T) {
	for _, h := range []int{240, 288} {
		m := New()
		m.ListMode = true
		m.Rows = visibleRows(640, h)
		m.Current().Page.Items = []jellyfin.Item{{Name: "Movie", Type: "Movie"}}
		pixels := render(640, h, m, "", Artwork{}, "", Animation{}, time.Date(2026, 1, 1, 12, 34, 0, 0, time.UTC))
		sy := safeY(640, h)
		if (h == 240 && (sy != 12 || m.Rows != 6)) || (h == 288 && (sy != 14 || m.Rows != 7)) {
			t.Fatalf("CRT geometry: height=%d margin=%d rows=%d", h, sy, m.Rows)
		}
		i := ((sy+21)*640 + 20) * 4
		if pixels[i] != 0x7c || pixels[i+1] != 0x37 || pixels[i+2] != 0x0d {
			t.Fatal("selection placement or palette changed")
		}
		m.Key("back")
		dialog := render(640, h, m, "", Artwork{}, "", Animation{}, time.Time{})
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
	frame := render(640, 240, m, "", Artwork{}, "", Animation{}, now)
	m.Current().Page.Items[0].CollectionType = "tvshows"
	sameName := render(640, 240, m, "", Artwork{}, "", Animation{}, now)
	for i := range frame {
		if frame[i] != sameName[i] {
			t.Fatal("library type changed the displayed name")
		}
	}
	m.Current().Page.Items[0].Name = "Family Cinema 4K"
	otherName := render(640, 240, m, "", Artwork{}, "", Animation{}, now)
	for i := range frame {
		if frame[i] != otherName[i] {
			t.Fatal("carousel name exceeds the C client's 160-pixel limit")
		}
	}
}
