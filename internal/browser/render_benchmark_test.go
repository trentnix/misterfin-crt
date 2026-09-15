package browser

import (
	"image"
	"image/color"
	"image/draw"
	"misterfin-crt/internal/jellyfin"
	"testing"
	"time"
)

func benchmarkScene() (*Model, Artwork) {
	m := New()
	m.Current().Page.Items = []jellyfin.Item{{Name: "Movies", CollectionType: "movies"}, {Name: "Television", CollectionType: "tvshows"}, {Name: "Music", CollectionType: "music"}}
	backdrop := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	draw.Draw(backdrop, backdrop.Bounds(), image.NewUniform(color.RGBA{80, 120, 160, 255}), image.Point{}, draw.Src)
	cover := image.NewRGBA(image.Rect(0, 0, 400, 600))
	draw.Draw(cover, cover.Bounds(), image.NewUniform(color.RGBA{160, 100, 70, 255}), image.Point{}, draw.Src)
	return m, Artwork{Backdrop: backdrop, Primary: cover, Covers: []image.Image{cover, backdrop, cover}}
}
func BenchmarkBrowserFrame(b *testing.B) {
	for _, list := range []bool{true, false} {
		name := "carousel"
		if list {
			name = "list"
		}
		b.Run(name, func(b *testing.B) {
			m, art := benchmarkScene()
			m.ListMode = list
			var renderer Renderer = NewRenderer()
			scene := sceneFromModel(m, PlaybackPresentation{}, SetupPresentation{}, selectionData{artwork: art}, "", time.Unix(100, 0))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				scene.Now = time.Unix(100, 0).Add(time.Duration(i) * time.Second / 30)
				renderer.Render(640, 240, scene)
			}
		})
	}
}

func BenchmarkCustomBackgroundFrame(b *testing.B) {
	for _, list := range []bool{false, true} {
		name := "carousel"
		if list {
			name = "list"
		}
		b.Run(name, func(b *testing.B) {
			m, art := benchmarkScene()
			m.ListMode = list
			scene := sceneFromModel(m, PlaybackPresentation{}, SetupPresentation{}, selectionData{artwork: art}, "", time.Unix(100, 0))
			scene.Background = art.Backdrop
			renderer := NewRenderer()
			renderer.Render(640, 240, scene)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				scene.Now = time.Unix(100, 0).Add(time.Duration(i) * time.Second / 30)
				renderer.Render(640, 240, scene)
			}
		})
	}
}
