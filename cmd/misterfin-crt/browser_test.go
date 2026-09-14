package main

import (
	"fmt"
	"path/filepath"
	"testing"

	"misterfin-crt/internal/input/evdev"
	"misterfin-crt/internal/platform"
	"misterfin-crt/internal/playback"
)

func TestBrowserStartupPreservesDecoderDefaults(t *testing.T) {
	t.Setenv("MISTERFIN_FB", "")
	t.Setenv("MISTERFIN_FRAME_OUT", "")
	mplayer := playback.DecoderConfig{Kind: playback.DecoderMPlayer}
	ffplay := playback.DecoderConfig{Kind: playback.DecoderFFplay}
	video := playback.DecoderConfig{Kind: playback.DecoderPython, Helper: "video.py"}
	audio := playback.DecoderConfig{Kind: playback.DecoderPython, Helper: "audio.py"}
	for _, tc := range []struct {
		name         string
		args         []string
		video, audio playback.DecoderConfig
		frames       string
	}{
		{"MiSTer", nil, mplayer, mplayer, ""},
		{"MiSTer override", []string{"-player=custom-mplayer"}, playback.DecoderConfig{Player: "custom-mplayer"}, playback.DecoderConfig{Player: "custom-mplayer"}, ""},
		{"desktop", []string{"-headless=640x240"}, ffplay, ffplay, ""},
		{"desktop override", []string{"-headless=640x240", "-player=custom-ffplay"}, playback.DecoderConfig{Kind: playback.DecoderFFplay, Player: "custom-ffplay"}, playback.DecoderConfig{Kind: playback.DecoderFFplay, Player: "custom-ffplay"}, ""},
		{"inline video and audio", []string{"-headless=640x240", "-output=frame.raw", "-terminal-player=video.py"}, video, video, "frame.raw.video"},
		{"separate audio helper", []string{"-headless=640x240", "-output=frame.raw", "-terminal-player=video.py", "-audio-player=audio.py"}, video, audio, "frame.raw.video"},
		{"audio helper only", []string{"-headless=640x240", "-audio-player=audio.py"}, ffplay, audio, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, err := parseOptions(append([]string{"-browse", "-device=/dev/test-fb"}, tc.args...))
			if err != nil {
				t.Fatal(err)
			}
			// Interlaced scanout must use physical geometry, not logical UI height.
			g := platform.Geometry{Width: 640, Height: 240, OutputWidth: 640, OutputHeight: 480}
			target := selectBrowserTarget(targetPresenter{geometry: g}, o, evdev.Config{})
			got := target.player
			if (target.activate != nil) != (o.headless == "") {
				t.Fatal("environment coordination must be limited to MiSTer")
			}
			wantOutput := "*companion.Backend"
			if o.headless == "" {
				wantOutput = "*native.Backend"
			} else if o.terminalPlayer != "" {
				wantOutput = "*framefile.Backend"
			}
			if fmt.Sprintf("%T", target.output) != wantOutput || target.readInput == nil {
				t.Fatal("incorrect target assembly")
			}
			if got.VideoDecoder != tc.video || got.AudioDecoder != tc.audio || got.FrameOutput != tc.frames {
				t.Fatalf("video=%+v audio=%+v frames=%q", got.VideoDecoder, got.AudioDecoder, got.FrameOutput)
			}
			if got.Width != 640 || got.Height != 480 || got.Device != "/dev/test-fb" {
				t.Fatal("lost physical output configuration")
			}
		})
	}
}

func TestBrowserStartupResolvesStorage(t *testing.T) {
	cache, state, override := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("XDG_CONFIG_HOME", state)
	for _, tc := range []struct {
		name, headless, override, stateDir, cacheRoot string
	}{
		{"MiSTer", "", "", "", "/media/fat"},
		{"desktop", "640x240", "", "", cache},
		{"MiSTer override", "", override, state, override},
		{"desktop override", "640x240", override, state, override},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MISTERFIN_CACHE_ROOT", tc.override)
			got, err := browserConfig(launchOptions{config: "server.conf", headless: tc.headless, stateDir: tc.stateDir})
			if err != nil {
				t.Fatal(err)
			}
			wantState := tc.stateDir
			if wantState == "" {
				wantState = filepath.Join(state, "misterfin-crt")
			}
			if got.ConfigPath != "server.conf" || got.StateDir != wantState {
				t.Fatalf("config: %+v", got)
			}
			if got.ArtworkCacheDir != filepath.Join(tc.cacheRoot, "misterfin-crt", "covercache") || got.MosaicCacheDir != filepath.Join(tc.cacheRoot, "misterfin-crt", "gridcache") {
				t.Fatalf("cache: %+v", got)
			}
		})
	}
}

func TestMissingUserCacheDirectoryDisablesOnlyCaching(t *testing.T) {
	t.Setenv("MISTERFIN_CACHE_ROOT", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")
	o := launchOptions{headless: "640x240", stateDir: t.TempDir()}
	got, err := browserConfig(o)
	if err != nil || got.ArtworkCacheDir != "" || got.MosaicCacheDir != "" || got.StateDir != o.stateDir {
		t.Fatalf("%+v: %v", got, err)
	}
	root := t.TempDir()
	t.Setenv("MISTERFIN_CACHE_ROOT", root)
	got, err = browserConfig(o)
	if err != nil || got.ArtworkCacheDir != filepath.Join(root, "misterfin-crt", "covercache") {
		t.Fatalf("override: %+v: %v", got, err)
	}
}

// Construction needs only geometry. Any attempted presentation panics through
// the embedded nil interface, so this fixture also checks assembly performs no I/O.
type targetPresenter struct {
	platform.Presenter
	geometry platform.Geometry
}

func (p targetPresenter) Geometry() platform.Geometry { return p.geometry }
