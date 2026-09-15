package browser

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/ui"
)

func TestBackgroundConfiguration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "background.json")
	if im, err := LoadBackground(path); err != nil || im != nil {
		t.Fatal("missing config changed default", err)
	}
	for _, config := range []string{`{}`, `{"image":""}`} {
		if err := os.WriteFile(path, []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		if im, err := LoadBackground(path); err != nil || im != nil {
			t.Fatal("empty image changed default", err)
		}
	}
	for _, config := range []string{`null`, `[]`, `{"unknown":1}`, `{} {}`, `{"image":"missing.png"}`} {
		if err := os.WriteFile(path, []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadBackground(path); err == nil {
			t.Fatalf("accepted %s", config)
		}
	}
	palette := image.NewPaletted(image.Rect(0, 0, 8, 6), color.Palette{color.RGBA{200, 40, 20, 255}})
	for _, format := range []string{"png", "jpeg"} {
		var encoded bytes.Buffer
		if format == "png" {
			_ = png.Encode(&encoded, palette)
		} else {
			_ = jpeg.Encode(&encoded, palette, nil)
		}
		asset := filepath.Join(dir, "art."+format)
		if err := os.WriteFile(asset, encoded.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{filepath.Base(asset), asset} {
			config, _ := json.Marshal(map[string]string{"image": name})
			if err := os.WriteFile(path, config, 0600); err != nil {
				t.Fatal(err)
			}
			im, err := LoadBackground(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := im.(*image.RGBA); !ok || im.Bounds() != palette.Bounds() {
				t.Fatal("image was not normalized")
			}
		}
	}
	var wide bytes.Buffer
	_ = png.Encode(&wide, image.NewRGBA(image.Rect(0, 0, 2049, 1)))
	asset := filepath.Join(dir, "too-wide.png")
	_ = os.WriteFile(asset, wide.Bytes(), 0600)
	if _, err := loadBackgroundImage(asset); err == nil {
		t.Fatal("unbounded image accepted")
	}
}

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
		scene := sceneFromModel(m, PlaybackPresentation{}, SetupPresentation{}, selectionData{artwork: art}, "", now)
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
				s.View.Detail = &jellyfin.Item{Type: "Movie", Name: "Movie"}
			case "setup":
				s.Setup.Kind = SetupConnecting
			case "about":
				s.About.Visible = true
			case "video":
				s.Video = true
				s.View.Detail = &jellyfin.Item{Type: "Movie"}
			case "music":
				s.Audio = true
				s.View.Detail = &jellyfin.Item{Type: "Audio"}
			case "photo":
				s.View.Detail = &jellyfin.Item{Type: "Photo"}
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
