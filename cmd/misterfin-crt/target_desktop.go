package main

import (
	"context"

	"misterfin-crt/internal/diagnostics"
	"misterfin-crt/internal/input"
	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/platform"
	"misterfin-crt/internal/playback"
	"misterfin-crt/internal/player/ffplay"
	"misterfin-crt/internal/player/pythonhelper"
	"misterfin-crt/internal/sound/alsa"
	"misterfin-crt/internal/videoout"
	"misterfin-crt/internal/videoout/companion"
	"misterfin-crt/internal/videoout/framefile"
)

// desktopTarget combines terminal input with either frame-file video for
// Ghostty or a companion player window. Both use the shared browser renderer.
func desktopTarget(d platform.Presenter, o launchOptions) browserTarget {
	config := desktopPlayback(o, d.Geometry())
	var output videoout.Output
	if o.terminalPlayer != "" {
		output = framefile.New(d, inlineFramePath(o))
	} else {
		output = companion.New(d)
	}
	return browserTarget{openSound: alsa.Open, player: config, output: output, readInput: func(ctx context.Context, _ *diagnostics.Log) (<-chan control.Event, <-chan struct{}, error) {
		return input.ReadTerminal(ctx)
	}}
}

// desktopPlayback defaults to FFplay and selects the Python helper when inline
// video is requested. An audio helper overrides audio only when no explicit
// player executable was supplied.
func desktopPlayback(o launchOptions, g platform.Geometry) playback.Config {
	decoder := ffplay.Decoder{Player: o.player}
	config := playback.Config{VideoDecoder: decoder, AudioDecoder: decoder, Height: g.OutputHeight}
	if o.terminalPlayer != "" {
		config.VideoDecoder = pythonhelper.Decoder{Script: o.terminalPlayer, Output: inlineFramePath(o), Width: g.OutputWidth, Height: g.OutputHeight}
		config.AudioDecoder = config.VideoDecoder
	}
	if o.audioPlayer != "" && o.player == "" {
		config.AudioDecoder = pythonhelper.Decoder{Script: o.audioPlayer}
	}
	return config
}

// inlineFramePath joins the Python decoder and frame-file output at one destination.
func inlineFramePath(o launchOptions) string { return o.output + ".video" }
