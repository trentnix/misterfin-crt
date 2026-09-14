package main

import (
	"context"
	"errors"

	"misterfin-crt/internal/browser"
	"misterfin-crt/internal/input"
	"misterfin-crt/internal/platform"
	"misterfin-crt/internal/playback"
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
	target := selectBrowserTarget(d, o, bindings)
	video, player := target.output, target.player
	defer func() { err = errors.Join(err, video.Close()) }()
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
	return browser.Run(ctx, config, player, video, browser.NewRenderer(), keys)
}
