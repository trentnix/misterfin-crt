package browser

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/ui"
)

func TestSetupRenderingAndConfiguredControls(t *testing.T) {
	var cache sceneCache
	for _, height := range []int{240, 288} {
		for kind := SetupConnecting; kind <= SetupSessionUnavailable; kind++ {
			for _, labels := range []control.Labels{control.KeyboardLabels(), {"open": "Cross", "back": "Circle"}, {"back": "Back"}} {
				setup := SetupPresentation{Kind: kind, Path: "/media/fat/misterfin-crt/interlaced-test/jellyfin.conf"}
				if kind == SetupQuickConnect {
					setup.Code = "123456"
					setup.Path = ""
				}
				if kind == SetupConnecting || kind == SetupCodeExpired {
					setup.Path = ""
				}
				if kind == SetupSessionUnavailable {
					setup.Path = "/media/fat/misterfin-crt/state"
				}
				scene := Scene{Setup: setup, Controls: labels, Now: time.Unix(100, 0)}
				c := ui.New(640, height)
				pixels := renderScene(c, &cache, scene, Animation{})
				fresh := renderScene(ui.New(640, height), nil, scene, Animation{})
				if !bytes.Equal(pixels, fresh) {
					t.Fatal("cached setup frame differs")
				}
				var hints []controlHint
				if action := setup.retryLabel(); action != "" {
					hints = append(hints, hint(labels, "open", action))
				}
				hints = append(hints, hint(labels, "back", "Exit"))
				rows := controlRows(640, hints)
				bottom := height - 8 - safeY(640, height)
				expected := ui.New(640, height)
				expected.Rect(0, 0, 640, height, 0x0b0d13)
				drawControls(expected, bottom, rows)
				start := controlsTop(bottom, rows) * 640 * 4
				if !bytes.Equal(pixels[start:], expected.Pixels[start:]) {
					t.Fatal("setup did not use shared badges or content overlaps controls")
				}
				if dir := os.Getenv("SETUP_PREVIEW_DIR"); dir != "" && labels.Name("open") == "Enter" {
					writeSetupPreview(t, dir, fmt.Sprintf("%d-%d.png", height, kind), c)
				}
			}
		}
	}
}

func TestSetupWaitingAnimationDoesNotMoveTheCode(t *testing.T) {
	s := Scene{Setup: SetupPresentation{Kind: SetupQuickConnect, Code: "123456"}, Controls: control.KeyboardLabels()}
	first := renderScene(ui.New(640, 240), nil, s, Animation{})
	next := renderScene(ui.New(640, 240), nil, s, Animation{Seconds: .3})
	if bytes.Equal(first, next) {
		t.Fatal("waiting indicator did not animate")
	}
	for y := 0; y < 195; y++ {
		if !bytes.Equal(first[y*640*4:(y+1)*640*4], next[y*640*4:(y+1)*640*4]) {
			t.Fatal("approval code or instructions moved")
		}
	}
}

func TestLongSetupPathAndLabelsStayAboveControls(t *testing.T) {
	s := Scene{Setup: SetupPresentation{Kind: SetupConfigInvalid, Path: "/root/" + strings.Repeat("long directory/", 30) + "jellyfin.conf"}, Controls: control.Labels{"open": strings.Repeat("X", 40), "back": strings.Repeat("Y", 40)}}
	pixels := renderScene(ui.New(640, 240), nil, s, Animation{})
	rows := controlRows(640, []controlHint{hint(s.Controls, "open", "Retry"), hint(s.Controls, "back", "Exit")})
	expected := ui.New(640, 240)
	expected.Rect(0, 0, 640, 240, 0x0b0d13)
	drawControls(expected, 220, rows)
	start := controlsTop(220, rows) * 640 * 4
	if !bytes.Equal(pixels[start:], expected.Pixels[start:]) {
		t.Fatal("long path overlaps controls")
	}
}

// writeSetupPreview exports a physically proportioned image only when requested
// during visual review. It never runs during ordinary tests or in the product.
func writeSetupPreview(t *testing.T, dir, name string, c *ui.Canvas) {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	im := image.NewRGBA(image.Rect(0, 0, c.Width, c.Height*2))
	for y := 0; y < im.Bounds().Dy(); y++ {
		for x := 0; x < c.Width; x++ {
			src := (y/2*c.Width + x) * 4
			dst := y*im.Stride + x*4
			im.Pix[dst], im.Pix[dst+1], im.Pix[dst+2], im.Pix[dst+3] = c.Pixels[src+2], c.Pixels[src+1], c.Pixels[src], 255
		}
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, im); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
