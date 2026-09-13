package playback

// Control requests an action on the active decoder. Unknown kinds are ignored.
type Control struct {
	// Kind is "pause", "refresh", "subtitle", "picture", or "seek" (relative audio seek).
	Kind    string
	Request int         // Correlates subtitle or picture replies with the latest selection.
	Index   int         // Jellyfin stream index for a subtitle request.
	Seconds int         // Signed offset for seek. Video seeks use stream replacement.
	Picture PictureMode // Requested live picture mode.
}

// Options configures one call to [Run]. The caller must keep referenced values
// unchanged until Run returns. All callbacks are optional. Except for CleanupDone,
// they run synchronously on Run's goroutine. Callbacks must return promptly and
// must not wait for Run to end.
type Options struct {
	// Preferences remembers per-video choices across playback and app restarts.
	// Nil disables persistence. The caller owns its lifetime.
	Preferences *Preferences
	// Tracks carries recorded-video choices across stream replacements. Nil
	// restores saved choices, or uses defaults when none have been saved.
	Tracks      *TrackOptions
	savedTracks *videoPreference
	// TrackInfo publishes source metadata before the decoder start gate.
	TrackInfo func(VideoTracks)
	// Subtitle reports an asynchronous text selection on the playback loop.
	Subtitle func(SubtitleResult)
	// Picture reports the decoder's acknowledgment of a live mode change.
	Picture     func(PictureResult)
	livePicture bool
	burnText    bool // Decoder cannot display the shared Go overlay on its video.

	// Levels receives disposable stereo audio levels on the playback loop.
	Levels      func(AudioLevels)
	audioExport string
	// StartTicks overrides the saved video position in 100-nanosecond ticks.
	// Nil resumes normally. A pointer to zero restarts. Audio and Live TV ignore it.
	StartTicks *int64
	// Ready runs after the source opens, before waiting on Start. It does not
	// mean the decoder has started or displayed its first frame.
	Ready func()
	// Start gates decoder launch. Nil starts immediately. The caller closes
	// or sends on the channel to proceed. Context cancellation aborts the wait.
	Start <-chan struct{}
	// AsyncCleanup permits final reporting and tuner release to outlive Run when this
	// channel can be received from at teardown. Nil keeps reporting synchronous.
	// The caller closes it when stopping or handing off a seek. Decoder and stream
	// cleanup still complete before Run returns. Pending routine reports are
	// canceled during the handoff. Stop/save use a fresh bounded context.
	AsyncCleanup <-chan struct{}
	// CleanupDone runs once after final reports and tuner release, or before Run
	// returns if preparation failed before a session was created.
	// With AsyncCleanup enabled it can run on a background goroutine after Run
	// returns. It must not access mutable caller state without synchronization.
	CleanupDone func()
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
	// VideoDecoder selects the protocol for movies, episodes, and other video,
	// including Live TV. The caller resolves executable overrides and helpers.
	VideoDecoder DecoderConfig
	// AudioDecoder independently selects the protocol for Audio items. It does
	// not inherit VideoDecoder. Its zero value selects MPlayer.
	AudioDecoder DecoderConfig
	// FrameOutput is required for Python video. It is the complete path where
	// the decoder publishes clean BGRX frames, with no suffix added by playback.
	// The output backend must read the same path. Audio ignores this field.
	FrameOutput string
	// Device names the native framebuffer, normally /dev/fb0.
	Device string
	// Width and Height describe physical output pixels. MPlayer requires a width
	// of 640 and a height of 240, 288, 480, or 576. Python video requires 640x240
	// or 640x288. Height also selects Jellyfin's NTSC (240/480) or PAL stream profile.
	Width, Height int
}
