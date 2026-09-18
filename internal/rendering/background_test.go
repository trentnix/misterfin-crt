package rendering

import (
	"bytes"
	"image"
	"image/color"
	"testing"
	"time"

	"mistervision/internal/media"
	"mistervision/internal/ui"
)

func TestCustomBackgroundScopeAndCache(t *testing.T) {
	m, art := benchmarkScene()
	custom := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			custom.SetRGBA(x, y, color.RGBA{200, 40, 20, 255})
		}
	}
	now := time.Unix(100, 0)
	for _, h := range []int{240, 288, 480} {
		renderer := NewRenderer()
		scene := testScene(m, PlaybackPresentation{}, SetupPresentation{}, art, "", now)
		scene.Background = custom
		for _, list := range []bool{false, true, false} {
			scene.ListMode = list
			frame := renderer.Render(640, h, scene)
			// Outside titles, posters, selection rows, and footer badges.
			at := ((h-1)*640 + 639) * 4
			if frame.UI[at+2] == 0 || frame.UI[at+2] <= frame.UI[at+1] {
				t.Fatal("custom background missing")
			}
			prepared := renderer.cache.customBase
			renderer.Render(640, h, scene)
			if renderer.cache.customBase != prepared {
				t.Fatal("background was rescaled on a steady frame")
			}
		}
		// Details, setup, About, and playback use their own presentations.
		for _, kind := range []string{"details", "setup", "about", "video", "music", "photo"} {
			s := scene
			switch kind {
			case "details":
				s.Content.Detail = &media.Item{Type: "Movie", Name: "Movie"}
			case "setup":
				s.Setup.Kind = SetupConnecting
			case "about":
				s.About.Visible = true
			case "video":
				s.Video = true
				s.Content.Detail = &media.Item{Type: "Movie"}
			case "music":
				s.Audio = true
				s.Content.Detail = &media.Item{Type: "Audio"}
			case "photo":
				s.Content.Detail = &media.Item{Type: "Photo"}
			}
			with := append([]byte(nil), NewRenderer().Render(640, h, s).UI...)
			s.Background = nil
			without := NewRenderer().Render(640, h, s).UI
			if !bytes.Equal(with, without) {
				t.Fatalf("custom background leaked into %s", kind)
			}
		}
		// Removing the override must discard cached custom list composition.
		scene.ListMode = true
		renderer.Render(640, h, scene)
		scene.Background = nil
		got := renderer.Render(640, h, scene).UI
		want := NewRenderer().Render(640, h, scene).UI
		if !bytes.Equal(got, want) {
			t.Fatal("default artwork did not return")
		}
	}
}

func TestCustomBackgroundCropsInPhysicalFourByThreeSpace(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 16, 9))
	for y := 0; y < 9; y++ {
		for x := 0; x < 16; x++ {
			c := color.RGBA{R: 200, A: 255}
			if x < 2 || x >= 14 {
				c = color.RGBA{B: 200, A: 255}
			}
			source.SetRGBA(x, y, c)
		}
	}
	for _, h := range []int{240, 288, 480} {
		c := ui.New(640, h)
		drawCustomBackground(c, source)
		for _, x := range []int{0, 639} {
			at := (h/2*640 + x) * 4
			if c.Pixels[at] != 0 || c.Pixels[at+2] == 0 {
				t.Fatal("image was stretched or letterboxed instead of cropped")
			}
		}
	}
}
