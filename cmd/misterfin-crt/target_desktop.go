package main

import (
	"misterfin-crt/internal/input"
	"misterfin-crt/internal/platform"
	"misterfin-crt/internal/playback"
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
		output = framefile.New(d, config.FrameOutput)
	} else {
		output = companion.New(d)
	}
	return browserTarget{player: config, output: output, readInput: input.ReadTerminal}
}

// desktopPlayback defaults to FFplay and selects the Python helper when inline
// video is requested. An audio helper overrides audio only when no explicit
// player executable was supplied.
func desktopPlayback(o launchOptions, g platform.Geometry) playback.Config {
	config := basePlayback(o, g, playback.DecoderFFplay)
	if o.terminalPlayer != "" {
		config.VideoDecoder = playback.DecoderConfig{Kind: playback.DecoderPython, Helper: o.terminalPlayer}
		config.AudioDecoder = config.VideoDecoder
		config.FrameOutput = o.output + ".video"
	}
	if o.audioPlayer != "" && o.player == "" {
		config.AudioDecoder = playback.DecoderConfig{Kind: playback.DecoderPython, Helper: o.audioPlayer}
	}
	return config
}
