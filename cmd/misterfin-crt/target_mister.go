package main

import (
	"context"

	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/input/evdev"
	"misterfin-crt/internal/mister/bgm"
	"misterfin-crt/internal/platform"
	"misterfin-crt/internal/playback"
	"misterfin-crt/internal/sound/alsa"
	"misterfin-crt/internal/videoout/native"
)

// misterTarget combines evdev input, the patched MPlayer, and native output.
// Shared browsing and rendering receive only semantic input and finished frames.
func misterTarget(d platform.Presenter, o launchOptions, bindings evdev.Config) browserTarget {
	return browserTarget{openSound: alsa.Open,
		activate: bgm.Suspend,
		player:   misterPlayback(o, d.Geometry()),
		output:   native.New(d, native.OverlayPath),
		readInput: func(ctx context.Context) (<-chan control.Event, <-chan struct{}, error) {
			return evdev.Read(ctx, bindings)
		},
	}
}

// misterPlayback selects the patched MPlayer for audio and video using the
// physical framebuffer dimensions supplied by the presenter.
func misterPlayback(o launchOptions, g platform.Geometry) playback.Config {
	return basePlayback(o, g, playback.DecoderMPlayer)
}
