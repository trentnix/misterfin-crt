package playback

import "mistervision/internal/media"

// Request describes one playback attempt, including a replacement after a seek
// or track change. Run reads Request without modifying it. The caller must keep
// referenced items, track choices, and callback values unchanged until Run returns.
type Request struct {
	// Item identifies the media and selects the initial decoder protocol.
	// Run refreshes its metadata from server before opening the stream.
	Item media.Item
	// Tracks carries current choices across replacements. Nil restores saved
	// choices, or uses defaults when none have been saved. Text is immutable.
	Tracks *TrackOptions
	// StartTicks overrides the saved position in 100-nanosecond ticks. Nil
	// resumes normally. A pointer to zero restarts. Audio and Live TV ignore it.
	StartTicks *int64
	// Start gates decoder launch after the source opens and Callbacks.Ready
	// runs. Nil starts immediately. Sending or closing opens the gate.
	// Context cancellation aborts the wait.
	Start <-chan struct{}
	// Controls supplies decoder actions. Nil or closed disables actions
	// without stopping playback. The caller owns the channel.
	Controls <-chan Control
	// AsyncCleanup permits final reporting and tuner release to outlive Run
	// when this channel can be received from at teardown. Nil keeps cleanup
	// synchronous. Sending or closing enables the handoff. Decoder and local
	// stream cleanup always complete before Run returns.
	AsyncCleanup <-chan struct{}
	// Callbacks receives feedback and coordinates video output ownership.
	Callbacks Callbacks
}
