package main

import (
	"context"
	"errors"

	"misterfin-crt/internal/browser"
	"misterfin-crt/internal/input"
	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/input/evdev"
	"misterfin-crt/internal/platform"
	"misterfin-crt/internal/playback"
	"misterfin-crt/internal/videoout"
	"misterfin-crt/internal/videoout/companion"
	"misterfin-crt/internal/videoout/framefile"
	"misterfin-crt/internal/videoout/native"
)

// runBrowser owns input, preferences, and video output. It translates the CLI's
// MiSTer and desktop defaults into independent dependencies for the browser.
func runBrowser(ctx context.Context, d platform.Display, o launchOptions) (err error) {
	bindings, err := input.LoadConfig(o.inputConfig, o.config)
	if err != nil {
		return err
	}
	config, err := browserConfig(o)
	if err != nil {
		return err
	}
	player := decoderOptions(o, d.Geometry())
	var video videoout.Output = companion.New(d)
	if o.terminalPlayer != "" {
		video = framefile.New(d, player.FrameOutput)
	} else if o.headless == "" {
		video = native.New(d, native.OverlayPath)
	}
	defer func() { err = errors.Join(err, video.Close()) }()
	preferences := playback.NewPreferences(config.StateDir)
	defer func() { err = errors.Join(err, preferences.Close()) }()
	player.Preferences = preferences

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	readInput := input.ReadTerminal
	if o.headless == "" {
		readInput = func(ctx context.Context) (<-chan control.Event, <-chan struct{}, error) {
			return evdev.Read(ctx, bindings)
		}
	}
	keys, done, err := readInput(ctx)
	if err != nil {
		video.Clear()
		return err
	}
	defer func() { cancel(); <-done }()
	return browser.Run(ctx, config, player, video, browser.NewRenderer(), keys)
}

// decoderOptions resolves command-line helper precedence once. Playback receives
// explicit audio and video protocols, with no knowledge of the selected display.
func decoderOptions(o launchOptions, g platform.Geometry) playback.Options {
	decoder := playback.DecoderConfig{Kind: playback.DecoderMPlayer, Player: o.player}
	if o.headless != "" {
		decoder.Kind = playback.DecoderFFplay
	}
	player := playback.Options{
		VideoDecoder: decoder, AudioDecoder: decoder,
		Device: o.device, Width: g.OutputWidth, Height: g.OutputHeight,
	}
	if o.terminalPlayer != "" {
		player.VideoDecoder = playback.DecoderConfig{Kind: playback.DecoderPython, Helper: o.terminalPlayer}
		player.AudioDecoder = player.VideoDecoder
		player.FrameOutput = o.output + ".video"
	}
	if o.audioPlayer != "" && o.player == "" {
		player.AudioDecoder = playback.DecoderConfig{Kind: playback.DecoderPython, Helper: o.audioPlayer}
	}
	return player
}
