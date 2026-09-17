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

	"mistervision/internal/connection"
	"mistervision/internal/input/control"
	"mistervision/internal/ui"
)

func TestSetupRenderingAndConfiguredControls(t *testing.T) {
	var cache sceneCache
	for _, height := range []int{240, 288} {
		for kind := SetupConnecting; kind <= SetupFailure; kind++ {
			for _, labels := range []control.Labels{control.KeyboardLabels(), {"open": "Cross", "back": "Circle"}, {"back": "Back"}} {
				setup := SetupPresentation{Kind: kind, Title: "Example setup", Message: "Follow the server instructions.", Retry: "Retry", PathLabel: "Configuration file", Path: "/media/fat/mistervision/interlaced-test/jellyfin.conf"}
				if kind == SetupApproval {
					setup.BackToServers = true
					setup.Code = "123456"
					setup.Path = ""
				}
				if kind == SetupConnecting {
					setup.Path, setup.Retry = "", ""
				}
				scene := Scene{Setup: setup, Controls: labels, Now: time.Unix(100, 0)}
				c := ui.New(640, height)
				pixels := renderScene(c, &cache, scene, Animation{})
				fresh := renderScene(ui.New(640, height), nil, scene, Animation{})
				if !bytes.Equal(pixels, fresh) {
					t.Fatal("cached setup frame differs")
				}
				var hints []controlHint
				if action := setup.RetryLabel(); action != "" {
					hints = append(hints, hint(labels, "open", action))
				}
				back := "Exit"
				if setup.BackToServers {
					back = "Back"
				}
				hints = append(hints, hint(labels, control.About, "About"), hint(labels, "back", back))
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
	s := Scene{Setup: SetupPresentation{Kind: SetupApproval, Code: "123456"}, Controls: control.KeyboardLabels()}
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
	s := Scene{Setup: SetupPresentation{Kind: SetupFailure, Retry: "Retry", PathLabel: "Configuration file", Path: "/root/" + strings.Repeat("long directory/", 30) + "jellyfin.conf"}, Controls: control.Labels{"open": strings.Repeat("X", 40), "back": strings.Repeat("Y", 40)}}
	pixels := renderScene(ui.New(640, 240), nil, s, Animation{})
	rows := controlRows(640, []controlHint{hint(s.Controls, "open", "Retry"), hint(s.Controls, control.About, "About"), hint(s.Controls, "back", "Exit")})
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

func TestServerPickerScrollAndControlSafety(t *testing.T) {
	for _, height := range []int{240, 288} {
		var servers []connection.Server
		for i := 0; i < 12; i++ {
			servers = append(servers, connection.Server{ID: fmt.Sprint(i), Name: fmt.Sprintf("Server %02d %s", i, strings.Repeat("long name ", 20)), URL: "http://server.example/" + strings.Repeat("long-path/", 20)})
		}
		scene := Scene{Setup: SetupPresentation{Kind: SetupServers, Title: "Choose a server", Servers: servers}, Controls: control.KeyboardLabels()}
		first := renderScene(ui.New(640, height), nil, scene, Animation{})
		scene.Setup.Selected = 11
		var cache sceneCache
		last := renderScene(ui.New(640, height), &cache, scene, Animation{})
		fresh := renderScene(ui.New(640, height), nil, scene, Animation{})
		if bytes.Equal(first, last) || !bytes.Equal(last, fresh) {
			t.Fatal("picker did not scroll or cache changed pixels")
		}
		hints := []controlHint{pairedHint(scene.Controls, control.Up, control.Down, "Choose"), hint(scene.Controls, control.Open, "Select"), hint(scene.Controls, control.Select, "Scan again"), hint(scene.Controls, control.About, "About"), hint(scene.Controls, control.Back, "Exit")}
		rows := controlRows(640, hints)
		bottom := height - 8 - safeY(640, height)
		expected := ui.New(640, height)
		expected.Rect(0, 0, 640, height, 0x0b0d13)
		drawControls(expected, bottom, rows)
		start := controlsTop(bottom, rows) * 640 * 4
		if !bytes.Equal(last[start:], expected.Pixels[start:]) {
			t.Fatal("server content overlapped controls")
		}
		if dir := os.Getenv("SETUP_PREVIEW_DIR"); dir != "" {
			scene.Setup.Servers = []connection.Server{{ID: "a", Name: "Living room", URL: "http://192.168.1.100:8096"}, {ID: "b", Name: "Media archive", URL: "https://media.example/jellyfin"}, {ID: "c", Name: "Upstairs", URL: "http://192.168.1.101:8096"}}
			scene.Setup.Selected = 1
			c := ui.New(640, height)
			renderScene(c, nil, scene, Animation{})
			writeSetupPreview(t, dir, fmt.Sprintf("servers-%d.png", height), c)
		}
	}
}

func TestConnectionMenuLayout(t *testing.T) {
	for _, height := range []int{240, 288} {
		choices := []connection.Choice{{ID: "existing", Name: "Use existing connection", Description: "Choose a configured or remembered server", Children: []connection.Choice{{ID: "plex", Name: "Home Plex", Description: "Plex · http://192.168.1.100:32400"}}}, {ID: "jellyfin-new", Name: "Jellyfin", Description: "Find a server on your local network"}, {ID: "plex-new", Name: "Plex", Description: "Link your account and choose a server"}}
		scene := Scene{About: AboutPresentation{Visible: true, ConnectionsVisible: true, Connections: choices, ConnectionSelected: 2}, Controls: control.KeyboardLabels()}
		c := ui.New(640, height)
		pixels := renderScene(c, nil, scene, Animation{})
		hints := []controlHint{pairedHint(scene.Controls, control.Up, control.Down, "Choose"), hint(scene.Controls, control.Open, "Select"), hint(scene.Controls, control.Back, "Back")}
		rows := controlRows(640, hints)
		bottom := height - 8 - safeY(640, height)
		expected := ui.New(640, height)
		expected.Rect(0, 0, 640, height, 0x0b0d13)
		drawControls(expected, bottom, rows)
		start := controlsTop(bottom, rows) * 640 * 4
		if !bytes.Equal(pixels[start:], expected.Pixels[start:]) {
			t.Fatal("connection menu content overlaps input hints")
		}
		if dir := os.Getenv("SETUP_PREVIEW_DIR"); dir != "" {
			writeSetupPreview(t, dir, fmt.Sprintf("connections-%d.png", height), c)
		}
	}
}

func TestRecoveryPromptLayout(t *testing.T) {
	for _, height := range []int{240, 288} {
		s := Scene{Setup: SetupPresentation{Kind: SetupServers, Title: "Server address changed", Message: "Same server found at a new address.\nSelect to reconnect, or go back.", Servers: []connection.Server{{ID: "saved", Name: "Living room", URL: "http://192.168.1.208:8096"}}}, Controls: control.KeyboardLabels()}
		s.About.Connections = []connection.Choice{{Name: "Jellyfin"}}
		c := ui.New(640, height)
		pixels := renderScene(c, nil, s, Animation{})
		rows := controlRows(640, []controlHint{hint(s.Controls, control.Open, "Select"), hint(s.Controls, control.Select, "Scan again"), hint(s.Controls, control.About, "About"), hint(s.Controls, control.Back, "Back")})
		bottom := height - 8 - safeY(640, height)
		expected := ui.New(640, height)
		expected.Rect(0, 0, 640, height, 0x0b0d13)
		drawControls(expected, bottom, rows)
		start := controlsTop(bottom, rows) * 640 * 4
		if !bytes.Equal(pixels[start:], expected.Pixels[start:]) {
			t.Fatal("recovery prompt overlaps controls")
		}
		if dir := os.Getenv("SETUP_PREVIEW_DIR"); dir != "" {
			writeSetupPreview(t, dir, fmt.Sprintf("recovery-%d.png", height), c)
		}
	}
}

func TestAccountPickerLayout(t *testing.T) {
	for _, height := range []int{240, 288} {
		for _, empty := range []bool{false, true} {
			scene := Scene{Setup: SetupPresentation{Kind: SetupServers, Title: "Choose a Plex server", Message: "Signed in as Test Viewer.", SignIn: "Sign in with another account"}}
			scene.About.Connections = []connection.Choice{{Name: "Plex"}}
			hints := []controlHint{}
			if empty {
				scene.Setup.Message += "\nNo reachable servers. Scan again or use another account."
			} else {
				scene.Setup.Servers = []connection.Server{{ID: "home", Name: "Home Plex", URL: "http://192.168.1.100:32400"}}
				scene.Setup.Selected = 1
				hints = append(hints, pairedHint(scene.Controls, control.Up, control.Down, "Choose"))
			}
			hints = append(hints, hint(scene.Controls, control.Open, "Select"), hint(scene.Controls, control.Select, "Scan again"), hint(scene.Controls, control.About, "About"), hint(scene.Controls, control.Back, "Back"))
			c := ui.New(640, height)
			pixels := renderScene(c, nil, scene, Animation{})
			rows := controlRows(640, hints)
			bottom := height - 8 - safeY(640, height)
			expected := ui.New(640, height)
			expected.Rect(0, 0, 640, height, 0x0b0d13)
			drawControls(expected, bottom, rows)
			start := controlsTop(bottom, rows) * 640 * 4
			if !bytes.Equal(pixels[start:], expected.Pixels[start:]) {
				t.Fatal("account action overlaps controls")
			}
			if dir := os.Getenv("SETUP_PREVIEW_DIR"); dir != "" {
				writeSetupPreview(t, dir, fmt.Sprintf("plex-account-%d-empty-%t.png", height, empty), c)
			}
		}
	}
}
