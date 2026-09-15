package main

import (
	"context"
	"errors"
	"log/slog"

	"misterfin-crt/internal/browser"
	"misterfin-crt/internal/input"
	"misterfin-crt/internal/jellyfin"
	jellyfinremote "misterfin-crt/internal/jellyfin/remote"
	"misterfin-crt/internal/platform"
	"misterfin-crt/internal/playback"
	"misterfin-crt/internal/release"
	"misterfin-crt/internal/remote"
	"misterfin-crt/internal/rendering"
	"misterfin-crt/internal/settings"
	"misterfin-crt/internal/sound"
)

// runBrowser owns input, preferences, and video output around the shared browser.
// Target assembly supplies independent dependencies before any reader starts.
func runBrowser(ctx context.Context, d platform.Display, o launchOptions, trace *startupDiagnostics, source *settings.File) (err error) {
	trace.phase("input-config")
	inputSettings := source.Section("input")
	if o.inputConfig != "" {
		inputSettings = settings.Read(o.inputConfig, 64<<10, true)
	}
	bindings, err := input.ParseConfig(inputSettings)
	if err != nil {
		return err
	}
	trace.phase("browser-config")
	config, err := browserConfig(o, trace.log, source)
	if err != nil {
		return err
	}
	config.Remote = func(client *jellyfin.Client) remote.Source { return jellyfinremote.New(client) }
	config.Build = release.CurrentBuild()
	config.CheckUpdate = func(ctx context.Context) (release.Status, error) {
		return release.Check(ctx, config.Build.Version)
	}
	if trace.notice != "" {
		config.StartupNotices = append(config.StartupNotices, trace.notice)
	}
	trace.phase("sound-config")
	soundConfig, notice := browsingSounds(o, trace.log, source)
	if notice != "" {
		config.StartupNotices = append(config.StartupNotices, notice)
	}
	target := selectBrowserTarget(d, o, bindings)
	if target.activate != nil {
		restore := target.activate()
		defer restore()
	}
	video, player := target.output, target.player
	defer func() { err = errors.Join(err, video.Close()) }()
	trace.phase("sound-open")
	sounds, err := sound.New(soundConfig, target.openSound)
	if err != nil {
		trace.log.ConfigurationFallback("ui.navigation_sounds", "sounds-off", err)
		config.StartupNotices = append(config.StartupNotices, "Navigation sounds unavailable. Continuing without feedback.")
	}
	defer sounds.Close()
	var feedback sound.Feedback
	if sounds != nil {
		feedback = sounds
	}
	preferences := playback.NewPreferences(config.StateDir)
	defer func() { err = errors.Join(err, preferences.Close()) }()
	player.Preferences = preferences

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	trace.phase("input-open")
	trace.log.Record("input.backend", slog.Bool("terminal", o.headless != ""), slog.Int("configured_profiles", len(bindings.Profiles)))
	keys, done, err := target.readInput(ctx, trace.log)
	if err != nil {
		video.Clear()
		return err
	}
	defer func() { cancel(); <-done }()
	trace.phase("browser")
	return browser.Run(ctx, config, player, video, rendering.NewRenderer(), feedback, keys)
}
