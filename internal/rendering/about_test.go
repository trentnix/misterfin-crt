package rendering

import (
	"fmt"
	"mistervision/internal/connection"
	"mistervision/internal/release"
	"mistervision/internal/ui"
	"os"
	"reflect"
	"strings"
	"testing"

	"mistervision/internal/input/control"
)

func TestReleaseNotesWrapAndRemoveDecoration(t *testing.T) {
	lines := ReleaseNotes("## Changes\n[Read this](https://example.com) **first**.\n"+strings.Repeat("x", 80)+"\nBad\x1bcontrol", 320)
	text := strings.Join(lines, "\n")
	for _, unwanted := range []string{"https://", "**", "##", "\x1b"} {
		if strings.Contains(text, unwanted) {
			t.Fatal(text)
		}
	}
	for _, line := range lines {
		if len([]rune(line)) > (320-48)/8 {
			t.Fatalf("line exceeds width: %q", line)
		}
	}
	if !strings.Contains(text, "Read this first.") {
		t.Fatal(text)
	}
}

func TestReleaseNoteScrollGeometryMatchesFooter(t *testing.T) {
	a := AboutPresentation{CanInstall: true, Notes: make([]string, 100)}
	for _, height := range []int{240, 288, 480} {
		labels := control.Labels{control.Back: "An unusually long controller button", control.Up: "Up", control.Down: "Down"}
		layout := a.notesLayout(640, height, labels)
		if a.ScrollLimit(640, height, labels) != 100-layout.rows {
			t.Fatal("input and rendering scroll limits differ")
		}
		if layout.top+layout.rows*12 > layout.statusY {
			t.Fatal("notes overlap status")
		}
	}
}

func TestInstallationCompletionStatus(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		a := AboutPresentation{Installed: true, Restarting: automatic}
		want := "Installed. Reopen MiSTerVision."
		if automatic {
			want = "Update installed. Restarting..."
		}
		if got := a.Status(); got != want {
			t.Fatalf("completion status: %q, want %q", got, want)
		}
	}
}

func TestReleaseNotesPreserveParagraphsAndGlyphWidths(t *testing.T) {
	word := strings.Repeat("界\ufe0f", 34)
	lines := ReleaseNotes("# Heading\n\n"+word+"\n\nTail", 320)
	want := []string{"Heading", "", word, "", "Tail"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("paragraphs or glyph widths changed: %#v", lines)
	}
	for _, line := range ReleaseNotes(word+word, 320) {
		if ui.TextWidth(line) > 272 || strings.HasPrefix(line, "\ufe0f") {
			t.Fatalf("split glyph or oversized line: %q", line)
		}
	}
}

func TestAboutFailureFitsCompactDisplay(t *testing.T) {
	for _, labels := range []control.Labels{control.KeyboardLabels(), nil} {
		for _, size := range [][2]int{{320, 240}, {640, 240}, {640, 288}, {640, 480}} {
			w, h := size[0], size[1]
			scene := Scene{Controls: labels, About: AboutPresentation{
				Visible: true, Checked: true, CanInstall: true, ProfileAction: connection.ProfileChoose,
				ForgetLabel: "Forget user",
				Profile:     &connection.Profile{Name: "Test viewer"},
				Connections: []connection.Choice{{ID: "plex", Name: "Plex"}},
				Release:     release.Status{Available: true, Latest: "v1.2.1", HasBundle: true},
				Message:     "Could not check for updates. Check your internet connection, then try Check updates again.",
			}}
			c := ui.New(w, h)
			renderScene(c, &sceneCache{}, scene, Animation{})
			sy := safeY(w, h)
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					if x >= 24 && x < w-24 && y >= sy && y < h-sy {
						continue
					}
					i := (y*w + x) * 4
					if c.Pixels[i] != 0x13 || c.Pixels[i+1] != 0x0d || c.Pixels[i+2] != 0x0b {
						t.Fatalf("About escaped safe area at %dx%d (%d,%d)", w, h, x, y)
					}
				}
			}
			if w == 320 {
				for _, text := range []string{"MiSTerVision", "Version dev", "Trent Nix", "Based on MiSTerFin by Pudding", "Studio", "CC BY-NC 4.0. Components have", "separate licenses.", "Test viewer", "Check updates again."} {
					if !screenContainsText(c, text) {
						t.Fatalf("compact About lost %q", text)
					}
				}
			}
			if dir := os.Getenv("MISTERVISION_ABOUT_PREVIEWS"); dir != "" {
				writeSetupPreview(t, dir, fmt.Sprintf("about-%dx%d-%s.png", w, h, labels.Name(control.Open)), c)
			}
		}
	}
}

// screenContainsText matches the complete glyph mask in a text color. Requiring
// unlit pixels too prevents a solid button badge from passing as readable text.
func screenContainsText(c *ui.Canvas, text string) bool {
	return screenContainsScaledText(c, text, 1)
}

func screenContainsScaledText(c *ui.Canvas, text string, scale int) bool {
	glyphs := ui.New(ui.TextWidth(text)*scale, 8*scale)
	glyphs.TextScaled(0, 0, text, 0xffffff, glyphs.Width, scale)
	for _, color := range []uint32{titleColor, dimColor, 0xc0c0c0, 0xd0d0d0} {
		for y := 0; y <= c.Height-glyphs.Height; y++ {
			for x := 0; x <= c.Width-glyphs.Width; x++ {
				matches := true
				for gy := 0; gy < glyphs.Height && matches; gy++ {
					for gx := 0; gx < glyphs.Width; gx++ {
						j := (gy*glyphs.Width + gx) * 4
						i := ((y+gy)*c.Width + x + gx) * 4
						pixel := uint32(c.Pixels[i]) | uint32(c.Pixels[i+1])<<8 | uint32(c.Pixels[i+2])<<16
						if (glyphs.Pixels[j] != 0) != (pixel == color) {
							matches = false
							break
						}
					}
				}
				if matches {
					return true
				}
			}
		}
	}
	return false
}

func TestAboutProfileActionMatchesAvailableChoices(t *testing.T) {
	for _, tc := range []struct {
		action connection.ProfileAction
		want   string
	}{
		{connection.ProfileUnchanged, ""}, {connection.ProfileAdd, "Add user"}, {connection.ProfileChoose, "Switch profile"},
	} {
		scene := Scene{About: AboutPresentation{Visible: true, Checking: true, Profile: &connection.Profile{Name: "Only viewer"}, ProfileAction: tc.action}}
		c := ui.New(640, 240)
		renderScene(c, &sceneCache{}, scene, Animation{})
		for _, label := range []string{"Add user", "Switch profile"} {
			if screenContainsText(c, label) != (label == tc.want) {
				t.Errorf("profile action %q, wanted %q", label, tc.want)
			}
		}
	}
}

func TestAboutDisplaysAccountFailureDuringReleaseCheck(t *testing.T) {
	for _, size := range [][2]int{{320, 240}, {640, 240}, {640, 480}} {
		w, h := size[0], size[1]
		scene := Scene{About: AboutPresentation{
			Visible: true, Checking: true,
			AccountMessage: connection.SignInStorageTitle + ". " + connection.SignInStorageMessage,
			Release:        release.Status{Available: true, Latest: "v9.0.0"},
		}}
		canvas := ui.New(w, h)
		renderScene(canvas, &sceneCache{}, scene, Animation{})
		if !screenContainsText(canvas, connection.SignInStorageTitle) || !screenContainsText(canvas, "then retry.") || screenContainsText(canvas, "Checking for updates...") {
			t.Fatalf("account error was hidden at %dx%d", w, h)
		}
	}
}
