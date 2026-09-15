package main

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"

	"misterfin-crt/internal/browser"
	"misterfin-crt/internal/input"
	"misterfin-crt/internal/platform"
	"misterfin-crt/internal/playback"
	"misterfin-crt/internal/release"
	"misterfin-crt/internal/sound"
)

// runBrowser owns input, preferences, and video output around the shared browser.
// Target assembly supplies independent dependencies before any reader starts.
func runBrowser(ctx context.Context, d platform.Display, o launchOptions, trace *startupDiagnostics) (err error) {
	trace.phase("input-config")
	bindings, err := input.LoadConfig(o.inputConfig, o.config)
	if err != nil {
		return err
	}
	trace.phase("browser-config")
	config, err := browserConfig(o)
	if err != nil {
		return err
	}
	config.Diagnostics = trace.log
	config.Build = release.CurrentBuild()
	config.CheckUpdate = func(ctx context.Context) (release.Status, error) {
		return release.Check(ctx, config.Build.Version)
	}
	soundPath := o.soundConfig
	if soundPath == "" {
		soundPath = filepath.Join(filepath.Dir(o.config), "sounds.json")
	}
	trace.phase("sound-config")
	soundConfig, err := sound.LoadConfig(soundPath)
	if err != nil {
		return err
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
		return err
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
	return browser.Run(ctx, config, player, video, browser.NewRenderer(), feedback, keys)
}
