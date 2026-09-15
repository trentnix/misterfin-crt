package browser

import (
	"bytes"
	"image"
	"image/color"
	"testing"
	"time"
)

func TestSceneCacheMatchesFreshFrames(t *testing.T) {
	m, art := benchmarkScene()
	// Include transparency and nonzero subimage origins in the cached mosaic.
	source := image.NewRGBA(image.Rect(3, 5, 80, 90))
	for y := 5; y < 90; y++ {
		for x := 3; x < 80; x++ {
			a := uint8(x * y)
			source.SetRGBA(x, y, color.RGBA{a / 2, a / 3, a / 4, a})
		}
	}
	cover := source.SubImage(image.Rect(7, 9, 60, 70))
	art.Covers = append(art.Covers, cover)
	renderer := RasterRenderer{}
	for _, size := range [][2]int{{640, 240}, {640, 288}, {640, 240}} {
		for _, list := range []bool{false, true, false} {
			m.ListMode = list
			for _, seconds := range []float64{0, 0.1, 3.7, 10.5, 10.6, 10.7} {
				anim := Animation{Seconds: seconds, TitleSeconds: seconds, Selection: 0.4, Row: 0.4}
				want := render(size[0], size[1], m, SetupPresentation{}, art, "", anim, time.Unix(100, 0))
				got := renderCached(&renderer, size[0], size[1], m, SetupPresentation{}, art, "", anim, time.Unix(100, 0))
				if !bytes.Equal(got, want) {
					t.Fatalf("cache changed pixels: size=%v list=%v seconds=%v", size, list, seconds)
				}
			}
		}
	}
	// Replace images, change tile aspect, remove artwork, and enter a detail view.
	m.Current().Page.Items[0].CollectionType = "music"
	for _, next := range []Artwork{{Backdrop: cover, Primary: cover, Covers: []image.Image{cover}}, {}, art} {
		for _, detail := range []bool{false, true} {
			if detail {
				item := m.Current().Page.Items[0]
				m.Current().Detail = &item
			} else {
				m.Current().Detail = nil
			}
			for _, list := range []bool{true, false} {
				m.ListMode = list
				want := render(640, 240, m, SetupPresentation{}, next, "", Animation{Seconds: 4}, time.Time{})
				got := renderCached(&renderer, 640, 240, m, SetupPresentation{}, next, "", Animation{Seconds: 4}, time.Time{})
				if !bytes.Equal(got, want) {
					t.Fatalf("stale cache after artwork/view change: detail=%v list=%v", detail, list)
				}
			}
		}
	}
}

// Supply explicit animation coordinates to compare cached and uncached pixels.
func renderCached(r *RasterRenderer, w, h int, m *Model, setup SetupPresentation, art Artwork, artError string, anim Animation, now time.Time) []byte {
	r.prepare(w, h)
	return renderScene(r.canvas, &r.cache, sceneFromModel(m, PlaybackPresentation{}, setup, selectionData{artwork: art}, artError, now), anim)
}
