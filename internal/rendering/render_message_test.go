package rendering

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

	"mistervision/internal/input/control"
	"mistervision/internal/ui"
)

func TestErrorGuidanceFitsSafeArea(t *testing.T) {
	const message = "The stream did not start in time. Try playing it again. If this keeps happening, check your media server."
	for _, size := range [][2]int{{320, 240}, {640, 240}, {640, 288}, {640, 480}} {
		w, h := size[0], size[1]
		lines := messageLines(message, w-88, 6)
		if strings.Join(lines, " ") != message {
			t.Fatalf("guidance lost at %dx%d", w, h)
		}
		c := ui.New(w, h)
		drawNotice(c, "Playback didn't start", message, -1, 6)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if x >= 32 && x < w-32 {
					continue
				}
				i := (y*w + x) * 4
				if c.Pixels[i] != 0 || c.Pixels[i+1] != 0 || c.Pixels[i+2] != 0 {
					t.Fatal("error escaped horizontal safe area")
				}
			}
		}
		a := AboutPresentation{NotesVisible: true, CanInstall: true, Message: "Could not download the update. Check your internet connection, then retry. Existing installation kept."}
		layout := a.notesLayout(w, h, control.KeyboardLabels())
		last := layout.statusY + (len(messageLines(a.Status(), w-48, 6))-1)*10 + 8
		if last >= controlsTop(h-8-safeY(w, h), layout.controls) {
			t.Fatal("update guidance overlaps controls")
		}
		a.ManualInstall = true
		for _, row := range a.notesLayout(w, h, control.KeyboardLabels()).controls {
			for _, hint := range row {
				if hint.description == "Install" {
					t.Fatal("manual update advertises Install")
				}
			}
		}
		// Optional local previews make the actual raster output available for review.
		if dir := os.Getenv("MISTERVISION_ERROR_PREVIEWS"); dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			scenes := map[string]Scene{
				"timeout": {Now: time.Now(), Message: MessagePresentation{Header: "Playback didn't start", Text: message, Until: time.Now().Add(time.Minute)}},
				"update":  {About: a},
				"library": {Content: Content{Error: "Could not load this list. The media server reported a problem. Try again or check the server."}},
			}
			for name, scene := range scenes {
				scene.Controls = control.KeyboardLabels()
				scene.About.Visible = name == "update"
				data := NewRenderer().Render(w, h, scene).UI
				im := image.NewRGBA(image.Rect(0, 0, w, h))
				for i := 0; i < len(data); i += 4 {
					im.Pix[i], im.Pix[i+1], im.Pix[i+2], im.Pix[i+3] = data[i+2], data[i+1], data[i], 255
				}
				f, err := os.Create(filepath.Join(dir, fmt.Sprintf("%s-%dx%d.png", name, w, h)))
				if err != nil {
					t.Fatal(err)
				}
				err = png.Encode(f, im)
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

func TestLongNoticeRetainsItsLastLine(t *testing.T) {
	a, b := ui.New(320, 240), ui.New(320, 240)
	text := "Could not load these subtitles. Try again or choose another "
	drawNotice(a, "", text+"track.", -1, 6)
	drawNotice(b, "", text+"option.", -1, 6)
	if bytes.Equal(a.Pixels, b.Pixels) {
		t.Fatal("last line was discarded")
	}
}
