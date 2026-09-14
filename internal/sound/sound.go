package sound

// Cue identifies a semantic UI event, independent of physical input bindings.
type Cue uint8

const (
	// Navigate accompanies an actual selection change.
	Navigate Cue = iota
	// Confirm accompanies opening, returning, or changing a browsing mode.
	Confirm
)

// Feedback receives UI cues and yields the audio device to media playback.
// Play must not block the UI. Suspend may run concurrently on playback workers.
type Feedback interface {
	// Play requests a cue. Busy implementations may discard it rather than queue lag.
	Play(Cue)
	// Suspend silences cues and releases the device before returning. Each call
	// returns an idempotent release function. Overlapping players retain silence
	// until all releases complete, including failed and canceled launches.
	Suspend() func()
}

// Stream accepts interleaved stereo 48 kHz PCM. Methods must return promptly.
// Only the sound worker writes. Suspension and shutdown serialize Close with Write.
type Stream interface {
	// Write accepts as many frames as currently fit without waiting for playback.
	// Zero means retry later. n counts stereo frames, not individual samples.
	Write([]int16) (n int, err error)
	// Close drops queued audio and relinquishes the device without draining it.
	Close() error
}

// OpenFunc opens an optional audio device on the worker, never on the UI loop.
// Failure suppresses feedback temporarily without failing browsing or playback.
type OpenFunc func() (Stream, error)
