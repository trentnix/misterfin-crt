package main

import (
	"context"
	"errors"
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
func runBrowser(ctx context.Context, d platform.Display, o launchOptions) (err error) {
	bindings, err := input.LoadConfig(o.inputConfig, o.config)
	if err != nil {
		return err
	}
	config, err := browserConfig(o)
	if err != nil {
		return err
	}
	config.Build = release.CurrentBuild()
	config.CheckUpdate = func(ctx context.Context) (release.Status, error) {
		return release.Check(ctx, config.Build.Version)
	}
	soundPath := o.soundConfig
	if soundPath == "" {
		soundPath = filepath.Join(filepath.Dir(o.config), "sounds.json")
	}
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
	keys, done, err := target.readInput(ctx)
	if err != nil {
		video.Clear()
		return err
	}
	defer func() { cancel(); <-done }()
	return browser.Run(ctx, config, player, video, browser.NewRenderer(), feedback, keys)
}
