package rendering

// PlaybackPresentation is a value snapshot. It contains no decoder handles,
// mutable shared state or output-specific information. Tracks points to a new,
// immutable menu snapshot that later controller events cannot change.
type PlaybackPresentation struct {
	TracksAvailable bool
	Tracks          *TrackMenu
	Subtitle        string
	// Active remains true during a seek handoff. Audio identifies the media type,
	// independent of whether a music queue is waiting for its next track.
	Active bool
	Audio  bool

	Title           string
	PositionTicks   int64
	DurationTicks   int64
	Paused          bool
	ControlsVisible bool
	Seekable        bool

	DestinationTicks int64 // Absolute position, valid only when HasDestination is true.
	HasDestination   bool
	ShowDestination  bool // Two or more presses, while waiting for the seek deadline.
	WaitLabel        string
	Notice           string
}
