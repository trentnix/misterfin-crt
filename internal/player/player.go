// Package player defines executable protocols independently of playback sessions
// and display backends. Implementations live in subpackages and never import playback.
package player

import (
	"io"
	"syscall"

	"misterfin-crt/internal/jellyfin"
)

// Decoder holds immutable launch settings. The caller owns the process and its
// transports. The caller must serialize control methods on the playback loop.
type Decoder interface {
	// Name is a stable diagnostic label. It must not include paths or media data.
	Name() string
	// WithPicture returns immutable launch settings for one request. It must not
	// mutate the receiver, which may be shared by concurrent playback sessions.
	WithPicture(PictureMode) Decoder
	// Feedback creates a writer for one process's stdout and stderr. It must accept
	// concurrent writes, bound retained output, and discard unknown diagnostics.
	// emit receives normalized values. It must not block and must be serialized
	// by the writer. The caller retains ownership of process and pipe lifetimes.
	Feedback(emit func(Feedback)) io.Writer
	// Validate checks settings for the requested item without opening resources.
	Validate(jellyfin.Item) error
	// ClientSubtitles reports whether shared overlay text reaches the video.
	ClientSubtitles() bool
	// Executable returns a path or a name to resolve through PATH.
	Executable() string
	// Input selects pipe or local proxy transport for the requested item.
	Input(jellyfin.Item) Input
	// Args builds arguments from refreshed item metadata without opening resources.
	// An empty source selects file descriptor 3. Otherwise source is a local proxy URL.
	Args(jellyfin.Item, string) []string
	// Pause requests the supplied pause state. The caller must invoke it only
	// when that state changes because some protocols expose only a toggle.
	// Success means the transport accepted the command, without an acknowledgment.
	Pause(Control, bool) error
	// Poll requests a position update. The caller invokes it once per second.
	// Players that report progress continuously need no command.
	Poll(Control)
	// Refresh requests a paused-frame redraw. Unsupported refresh is a no-op.
	Refresh(Control)
}

// Input selects media transport. URL sources use an authenticated local proxy.
type Input uint8

const (
	// Pipe supplies media on descriptor 3.
	Pipe Input = iota
	// URL allows byte-range requests through the local proxy.
	URL
)

// Control borrows stdin and process-group signaling for one call. Implementations
// must not close or retain either transport.
type Control struct {
	// Stdin writes commands to the running player.
	Stdin io.Writer
	// Signal sends a signal to the isolated player process group.
	Signal func(syscall.Signal) error
}

// AudioSeeker optionally seeks a direct audio source without replacing it.
// Video seeking remains a playback-session concern.
type AudioSeeker interface {
	// Seek moves by a signed offset in seconds while preserving pause state.
	// Success means the command was sent. Position feedback arrives separately.
	Seek(Control, int) error
}

// PictureSetter optionally changes fit without replacing the running decoder.
// The request number is echoed in the player's ANS_PICTURE_MODE feedback.
type PictureSetter interface {
	// SetPicture requests a fit change while preserving position and pause state.
	// The request number correlates the later acknowledgment with this command.
	// A nil error confirms command delivery only.
	SetPicture(Control, PictureMode, int) error
}

// AudioLevels is a stereo RMS amplitude snapshot normalized to [0,1].
// Missing samples and silence produce zero. It carries no rendering policy.
type AudioLevels [2]float64

// Meter supplies optional sampled audio feedback. The caller must Close it after
// the decoder exits, or on any failure before launch. Nil means no sampled meter.
type Meter interface {
	// Levels samples current stereo amplitudes. Unavailable samples return zero.
	Levels() AudioLevels
	// Close releases sampling resources after the decoder stops writing them.
	Close()
}

// LevelConfigurer returns settings for one launch and optional owned resources.
// A nil Meter can mean feedback uses the status pipe or meters are unavailable.
// Implementations must return a nil interface when no resource was allocated.
type LevelConfigurer interface {
	// WithAudioLevels returns settings for one launch without changing the receiver.
	// The caller owns any returned Meter, including when launch fails.
	// Unavailable sampling must leave playback usable and return a nil Meter.
	WithAudioLevels() (Decoder, Meter)
}
