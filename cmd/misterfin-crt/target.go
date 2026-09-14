package main

import (
	"context"

	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/input/evdev"
	"misterfin-crt/internal/platform"
	"misterfin-crt/internal/playback"
	"misterfin-crt/internal/sound"
	"misterfin-crt/internal/videoout"
)

// browserTarget assembles independent input, player settings, and output.
// runBrowser owns their lifetimes. Assembly never starts input or playback.
type browserTarget struct {
	// activate acquires optional environment resources after configuration.
	// Its non-nil return releases them after browser, input, and output cleanup.
	// A nil activate means the target needs no environment coordination.
	activate func() func()
	// openSound borrows the target audio device on the sound worker.
	openSound sound.OpenFunc
	player    playback.Config
	output    videoout.Output
	readInput func(context.Context) (<-chan control.Event, <-chan struct{}, error)
}

// selectBrowserTarget chooses the native target unless a headless output was
// requested. The caller owns the returned input and output lifetimes.
func selectBrowserTarget(d platform.Presenter, o launchOptions, bindings evdev.Config) browserTarget {
	if o.headless == "" {
		return misterTarget(d, o, bindings)
	}
	return desktopTarget(d, o)
}

// basePlayback copies physical output geometry for stream and decoder setup.
func basePlayback(o launchOptions, g platform.Geometry, kind playback.DecoderKind) playback.Config {
	decoder := playback.DecoderConfig{Kind: kind, Player: o.player}
	return playback.Config{VideoDecoder: decoder, AudioDecoder: decoder, Device: o.device, Width: g.OutputWidth, Height: g.OutputHeight}
}
