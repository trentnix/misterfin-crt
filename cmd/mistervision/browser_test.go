package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"mistervision/internal/diagnostics"
	"mistervision/internal/input/evdev"
	"mistervision/internal/platform"
	"mistervision/internal/player"
	desktopplayer "mistervision/internal/player/ffplay"
	nativeplayer "mistervision/internal/player/mplayer"
	inlineplayer "mistervision/internal/player/pythonhelper"
	"mistervision/internal/settings"
)

func TestBrowserStartupPreservesDecoderDefaults(t *testing.T) {
	t.Setenv("MISTERVISION_FB", "")
	t.Setenv("MISTERVISION_FRAME_OUT", "")
	mplayer := nativeplayer.Decoder{Width: 640, Height: 480, Device: "/dev/test-fb"}
	ffplay := desktopplayer.Decoder{}
	video := inlineplayer.Decoder{Script: "video.py", Output: "frame.raw.video", Width: 640, Height: 480}
	audio := inlineplayer.Decoder{Script: "audio.py"}
	for _, tc := range []struct {
		name         string
		args         []string
		video, audio player.Decoder
		frames       string
	}{
		{"MiSTer", nil, mplayer, mplayer, ""},
		{"MiSTer override", []string{"-player=custom-mplayer"}, nativeplayer.Decoder{Player: "custom-mplayer", Device: "/dev/test-fb", Width: 640, Height: 480}, nativeplayer.Decoder{Player: "custom-mplayer", Device: "/dev/test-fb", Width: 640, Height: 480}, ""},
		{"desktop", []string{"-headless=640x240"}, ffplay, ffplay, ""},
		{"desktop override", []string{"-headless=640x240", "-player=custom-ffplay"}, desktopplayer.Decoder{Player: "custom-ffplay"}, desktopplayer.Decoder{Player: "custom-ffplay"}, ""},
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
			if got.VideoDecoder != tc.video || got.AudioDecoder != tc.audio {
				t.Fatalf("video=%+v audio=%+v frames=%q", got.VideoDecoder, got.AudioDecoder, tc.frames)
			}
			if tc.frames != "" && inlineFramePath(o) != tc.frames {
				t.Fatal("decoder/output frame paths differ")
			}
			if got.Height != 480 {
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
			t.Setenv("MISTERVISION_CACHE_ROOT", tc.override)
			got, err := browserConfig(launchOptions{config: "server.conf", headless: tc.headless, stateDir: tc.stateDir}, nil, mustSettings(t, launchOptions{config: "server.conf", headless: tc.headless, stateDir: tc.stateDir}))
			if err != nil {
				t.Fatal(err)
			}
			wantState := tc.stateDir
			if wantState == "" {
				wantState = filepath.Join(state, "mistervision")
			}
			if got.StateDir != wantState {
				t.Fatalf("config: %+v", got)
			}
			if got.ArtworkCacheDir != filepath.Join(tc.cacheRoot, "mistervision", "covercache") || got.MosaicCacheDir != filepath.Join(tc.cacheRoot, "mistervision", "gridcache") {
				t.Fatalf("cache: %+v", got)
			}
		})
	}
}

func TestMissingUserCacheDirectoryDisablesOnlyCaching(t *testing.T) {
	t.Setenv("MISTERVISION_CACHE_ROOT", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")
	o := launchOptions{headless: "640x240", stateDir: t.TempDir()}
	got, err := browserConfig(o, nil, mustSettings(t, o))
	if err != nil || got.ArtworkCacheDir != "" || got.MosaicCacheDir != "" || got.StateDir != o.stateDir {
		t.Fatalf("%+v: %v", got, err)
	}
	root := t.TempDir()
	t.Setenv("MISTERVISION_CACHE_ROOT", root)
	got, err = browserConfig(o, nil, mustSettings(t, o))
	if err != nil || got.ArtworkCacheDir != filepath.Join(root, "mistervision", "covercache") {
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

// Background failures must preserve browsing defaults without changing other configuration.
func TestBrowserStartupFallsBackFromUnavailableBackground(t *testing.T) {
	for _, data := range []string{`{"image":"missing.png"}`, `{"image":"invalid.png"}`, `not json`} {
		t.Run(data, func(t *testing.T) {
			dir := t.TempDir()
			for name, contents := range map[string]string{
				"background.json": data,
				"invalid.png":     "not an image",
				"ui.json":         `{"title":"Trent's CRT"}`,
			} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600); err != nil {
					t.Fatal(err)
				}
			}
			config, err := browserConfig(launchOptions{config: filepath.Join(dir, "jellyfin.conf"), stateDir: dir}, nil, mustSettings(t, launchOptions{config: filepath.Join(dir, "jellyfin.conf"), stateDir: dir}))
			if err != nil {
				t.Fatal(err)
			}
			if config.Background != nil || len(config.StartupNotices) == 0 || config.Title == nil || *config.Title != "Trent's CRT" {
				t.Fatalf("fallback lost browsing settings: %+v", config)
			}
		})
	}
}

func TestOptionalBrowsingSettingsRecoverTogether(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string]string{"ui.json": `{"title":42}`, "background.json": `{"image":"missing.png"}`} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	config, err := browserConfig(launchOptions{config: filepath.Join(dir, "jellyfin.conf"), stateDir: dir}, nil, mustSettings(t, launchOptions{config: filepath.Join(dir, "jellyfin.conf"), stateDir: dir}))
	if err != nil || config.Title != nil || config.Background != nil || len(config.StartupNotices) != 2 {
		t.Fatalf("optional failures: %+v, %v", config, err)
	}
}

func TestBrowsingSoundDefaultsAndFailures(t *testing.T) {
	dir := t.TempDir()
	o := launchOptions{config: filepath.Join(dir, "jellyfin.conf")}
	config, notice := browsingSounds(o, nil, mustSettings(t, o))
	if !config.Enabled || config.Volume != 10 || notice != "" {
		t.Fatal("missing optional settings lost defaults")
	}
	o.soundConfig = filepath.Join(dir, "override.json")
	config, notice = browsingSounds(o, nil, mustSettings(t, o))
	if config.Enabled || notice == "" {
		t.Fatal("missing explicit override enabled sounds")
	}
	for _, tc := range []struct {
		data    string
		enabled bool
		volume  int
		invalid bool
	}{
		{`{}`, true, 10, false}, {`{"enabled":false}`, false, 10, false}, {`{"volume":0}`, true, 0, false},
		{`{"enabled":false,"volume":999}`, false, 0, true}, {`{"volume":99,"typo":true}`, false, 0, true},
		{`null`, false, 0, true}, {``, false, 0, true},
	} {
		if err := os.WriteFile(o.soundConfig, []byte(tc.data), 0600); err != nil {
			t.Fatal(err)
		}
		config, notice = browsingSounds(o, nil, mustSettings(t, o))
		if config.Enabled != tc.enabled || config.Volume != tc.volume || (notice != "") != tc.invalid {
			t.Fatalf("%s: %+v, %q", tc.data, config, notice)
		}
	}
}

func TestDefaultSettingsDoNotLogFallbacks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log")
	log, err := diagnostics.Open(diagnostics.Config{Enabled: true, Path: path, MaxBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	o := launchOptions{config: filepath.Join(dir, "jellyfin.conf"), stateDir: dir}
	if _, err := browserConfig(o, log, mustSettings(t, o)); err != nil {
		t.Fatal(err)
	}
	browsingSounds(o, log, mustSettings(t, o))
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatal("ordinary defaults recorded as configuration failures")
	}
}

// mustSettings loads a source without involving display or player resources.
func mustSettings(t *testing.T, o launchOptions) *settings.File {
	t.Helper()
	source, err := loadSettings(o)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestUnifiedSettingsAssemblyAndRelativePaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom-settings.json")
	data := `{"ui":{"title":"","navigation_sounds":{"enabled":false}},"music_visuals":{"default_background":"Off","show_audio_meters":false,"backgrounds":[{"name":"Off","type":"none"}]},"diagnostics":{"enabled":true,"path":"logs/events","max_bytes":4096},"display":{"interlaced":true}}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	o := launchOptions{browse: true, settingsPath: path, config: filepath.Join(t.TempDir(), "jellyfin.conf"), stateDir: t.TempDir()}
	source := mustSettings(t, o)
	config, err := browserConfig(o, nil, source)
	if err != nil {
		t.Fatal(err)
	}
	if config.Title == nil || *config.Title != "" || config.MusicVisuals == nil || config.MusicVisuals.Config.Default != "Off" || config.MusicVisuals.Config.Meters {
		t.Fatal("lost explicit title or music source")
	}
	sounds, notice := browsingSounds(o, nil, source)
	if sounds.Enabled || notice != "" {
		t.Fatal("lost explicit mute")
	}
	trace, err := openStartupDiagnostics(o, false, source)
	if err != nil {
		t.Fatal(err)
	}
	trace.close(nil)
	if _, err := os.Stat(filepath.Join(dir, "logs/events")); err != nil {
		t.Fatal("log did not resolve beside settings", err)
	}
}
