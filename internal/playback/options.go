package playback

// Control requests an action on the active decoder. Unknown kinds are ignored.
type Control struct {
	// Kind is "pause", "refresh", or "seek" (relative audio seek).
	Kind    string
	Seconds int // Signed offset for seek. Video seeks use stream replacement.
}

// Options configures one call to [Run]. The caller must keep referenced values
// unchanged until Run returns. All callbacks are optional and run synchronously
// on Run's goroutine. They must return promptly and must not wait for Run to end.
type Options struct {
	// StartTicks overrides the saved video position in 100-nanosecond ticks.
	// Nil resumes normally. A pointer to zero restarts. Audio and Live TV ignore it.
	StartTicks *int64
	// Ready runs after the source opens, before waiting on Start. It does not
	// mean the decoder has started or displayed its first frame.
	Ready func()
	// Start gates decoder launch. Nil starts immediately. The caller closes
	// or sends on the channel to proceed. Context cancellation aborts the wait.
	Start <-chan struct{}
	// AsyncCleanup permits final progress reporting to outlive Run when this
	// channel can be received from at teardown. Nil keeps reporting synchronous.
	// The caller normally closes it during a seek handoff. Decoder and stream
	// cleanup still complete before Run returns. Pending routine reports are
	// canceled during the handoff. Stop/save use a fresh bounded context.
	AsyncCleanup <-chan struct{}
	// AudioPlayer selects a Python audio helper when Player is empty.
	AudioPlayer string
	// Controls supplies decoder actions. Nil disables actions. Closing the
	// channel disables further actions without stopping playback.
	Controls <-chan Control
	// Paused reports a successfully issued pause or resume command, rather
	// than an acknowledgment that the decoder has completed the transition.
	Paused func(bool)
	// ControlError reports an unsupported or failed decoder command without ending playback.
	ControlError func(error)
	// Buffering forwards explicit decoder buffering feedback when available.
	Buffering func(bool)
	// VideoStarted reports the decoder's first presented video frame when the
	// output driver provides that signal. It does not imply a known position.
	VideoStarted func()
	// AcquireVideo runs after a video process starts, before stream copying.
	// ReleaseVideo pairs with it after process completion and stream-copy cleanup.
	// Audio invokes neither callback. ReleaseVideo requires AcquireVideo.
	AcquireVideo func()
	ReleaseVideo func()
	// Player overrides the default executable. Native playback defaults to
	// mplayer-arm. Headless playback defaults to FFplay.
	Player string
	// TerminalPlayer selects the Python inline decoder for headless video.
	// It requires FrameOutput and cannot be combined with Player.
	TerminalPlayer string
	// FrameOutput is the browser's raw frame path. The inline decoder publishes
	// clean video frames beside it with the ".video" suffix.
	FrameOutput string
	// Headless selects desktop player commands instead of native MiSTer commands.
	Headless bool
	// Device names the native framebuffer, normally /dev/fb0.
	Device string
	// Width and Height describe the physical output pixels, not UI layout pixels.
	Width, Height int
}
