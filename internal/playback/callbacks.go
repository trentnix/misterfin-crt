package playback

// Callbacks receives feedback from one playback attempt. Every callback is
// optional. Except for CleanupDone, callbacks run synchronously on Run's
// goroutine and must return promptly without waiting for Run to end. Callers
// forwarding progress or audio levels must avoid blocking on a slow consumer.
type Callbacks struct {
	// Position receives absolute Jellyfin positions in 100-nanosecond ticks.
	Position func(int64)
	// TrackInfo publishes source metadata before the decoder start gate.
	TrackInfo func(VideoTracks)
	// Subtitle reports completion of an asynchronous text selection.
	Subtitle func(SubtitleResult)
	// Caption receives the latest decoded live caption screen. Empty text clears it.
	// The video decoder controls timing. Text is independent of UI selection.
	Caption func(string)
	// Picture reports the decoder's acknowledgment of a live mode change.
	Picture func(PictureResult)
	// Levels receives disposable stereo audio levels on the playback loop.
	Levels func(AudioLevels)
	// Ready runs after the source opens, before waiting on Request.Start.
	// It does not mean the decoder has started or displayed its first frame.
	Ready func()
	// Paused reports a successfully issued pause/resume command. It does not
	// acknowledge that the decoder has completed the transition.
	Paused func(bool)
	// ControlError reports an unsupported or failed command without stopping playback.
	ControlError func(error)
	// Buffering forwards explicit decoder buffering feedback when available.
	Buffering func(bool)
	// VideoStarted reports the decoder's first presented video frame when
	// supported by its output driver. It does not imply a known position.
	VideoStarted func()
	// AcquireVideo runs after a video process starts, before stream copying.
	// Audio invokes neither AcquireVideo nor ReleaseVideo.
	AcquireVideo func()
	// ReleaseVideo pairs with AcquireVideo after decoder exit and stream-copy
	// cleanup. It is only called if AcquireVideo was supplied and called.
	ReleaseVideo func()
	// CleanupDone runs once after final reporting and tuner release, or before
	// Run returns if preparation fails before a session is created. Once a
	// session exists, it runs on the finalization worker. Run waits for the
	// callback unless Request.AsyncCleanup permits an early return. The
	// callback must synchronize access to shared caller state.
	CleanupDone func()
}
