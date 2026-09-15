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
