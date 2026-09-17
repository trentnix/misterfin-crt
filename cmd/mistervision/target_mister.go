package main

import (
	"context"

	"mistervision/internal/diagnostics"
	"mistervision/internal/input/control"
	"mistervision/internal/input/evdev"
	"mistervision/internal/mister/bgm"
	"mistervision/internal/platform"
	"mistervision/internal/playback"
	"mistervision/internal/player/mplayer"
	"mistervision/internal/sound/alsa"
	"mistervision/internal/videoout/native"
)

// misterTarget combines evdev input, the patched MPlayer, and native output.
// Shared browsing and rendering receive only semantic input and finished frames.
func misterTarget(d platform.Presenter, o launchOptions, bindings evdev.Config) browserTarget {
	return browserTarget{openSound: alsa.Open,
		activate: bgm.Suspend,
		player:   misterPlayback(o, d.Geometry()),
		output:   native.New(d, native.OverlayPath),
		readInput: func(ctx context.Context, log *diagnostics.Log) (<-chan control.Event, <-chan struct{}, error) {
			return evdev.Read(ctx, bindings, log)
		},
	}
}

// misterPlayback selects the patched MPlayer for audio and video using the
// physical framebuffer dimensions supplied by the presenter.
func misterPlayback(o launchOptions, g platform.Geometry) playback.Config {
	decoder := mplayer.Decoder{Player: o.player, Device: o.device, Width: g.OutputWidth, Height: g.OutputHeight}
	return playback.Config{VideoDecoder: decoder, AudioDecoder: decoder, Height: g.OutputHeight}
}
