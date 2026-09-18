package main

import (
	"context"

	"mistervision/internal/diagnostics"
	"mistervision/internal/input/control"
	"mistervision/internal/input/evdev"
	"mistervision/internal/platform"
	"mistervision/internal/playback"
	"mistervision/internal/sound"
	"mistervision/internal/videoout"
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
	// initialControls describes the selected input source before its first event.
	// Nil keeps the default controller legend until a device publishes bindings.
	initialControls control.Labels
	readInput       func(context.Context, *diagnostics.Log) (<-chan control.Event, <-chan struct{}, error)
}

// selectBrowserTarget chooses the native target unless a headless output was
// requested. The caller owns the returned input and output lifetimes.
func selectBrowserTarget(d platform.Presenter, o launchOptions, bindings evdev.Config) browserTarget {
	if o.headless == "" {
		return misterTarget(d, o, bindings)
	}
	return desktopTarget(d, o)
}

// crtPlaybackTiming preserves the CRT modes mirrored by the current targets.
// Other desktop sizes use the default 30 fps policy instead of implying PAL.
// A future destination can supply playback.Timing directly without this helper.
func crtPlaybackTiming(height int) playback.Timing {
	switch height {
	case 288, 576:
		return playback.Timing{PAL: true}
	case 480:
		// Match the core's approximately 59.94 Hz field clock without speeding video.
		return playback.Timing{LiveFrameRate: 30000.0 / 1001}
	default:
		return playback.Timing{}
	}
}
