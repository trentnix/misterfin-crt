package rendering

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"misterfin-crt/internal/ui"
)

func TestRootHeadingTruncatesBeforeClock(t *testing.T) {
	for _, width := range []int{320, 640} {
		for _, height := range []int{240, 288, 480} {
			long := strings.Repeat("Wide title ", 20)
			want := string([]rune(long)[:(width-108)/16-3]) + "..."
			for _, seconds := range []float64{0, 2, 20} {
				got, expected := ui.New(width, height), ui.New(width, height)
				scene := Scene{Root: true, Title: &long, Now: time.Unix(100, 0)}
				p := screenPainter{canvas: got, width: width, height: height, safeY: safeY(width, height), scene: scene, animation: Animation{TitleSeconds: seconds}}
				p.header(scene.title(), p.safeY+4)
				p.canvas = expected
				p.scene.Root = false
				p.header(want, p.safeY+4)
				if !bytes.Equal(got.Pixels, expected.Pixels) {
					t.Fatalf("heading overflowed or scrolled at %dx%d, time %v", width, height, seconds)
				}
			}
		}
	}
	if got := (Scene{Root: true}).title(); got != "MiSTerFin CRT" {
		t.Fatal(got)
	}
	custom := "Custom"
	if got := (Scene{Title: &custom, Content: Content{Title: "Library"}}).title(); got != "Library" {
		t.Fatal(got)
	}
}

func TestEmptyTitleHidesHeadingAndKeepsClock(t *testing.T) {
	title := ""
	for _, height := range []int{240, 288, 480} {
		for _, list := range []bool{false, true} {
			scene := Scene{Root: true, ListMode: list, Title: &title, Now: time.Unix(100, 0)}
			if scene.title() != "" {
				t.Fatal("empty title restored the default")
			}
			got, expected := ui.New(640, height), ui.New(640, height)
			p := screenPainter{canvas: got, width: 640, height: height, safeY: safeY(640, height), scene: scene}
			if list {
				p.list()
			} else {
				p.carousel()
			}
			p.canvas = expected
			p.clock()
			start, end := p.safeY*640*4, (p.safeY+20)*640*4
			if !bytes.Equal(got.Pixels[start:end], expected.Pixels[start:end]) {
				t.Fatalf("hidden heading changed clock or drew title pixels: height=%d list=%v", height, list)
			}
		}
	}
}
