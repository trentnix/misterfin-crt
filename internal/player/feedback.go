package player

// FeedbackKind identifies normalized decoder feedback. Unknown output is discarded
// by the decoder before it reaches playback.
type FeedbackKind uint8

const (
	FeedbackPosition     FeedbackKind = iota + 1 // Position is elapsed seconds in this source.
	FeedbackLevels                               // Levels contains normalized stereo amplitude.
	FeedbackBuffering                            // Buffering is the current cache-wait state.
	FeedbackVideoStarted                         // The decoder has presented its first video frame.
	FeedbackPicture                              // Picture acknowledges a requested picture-mode change.
	FeedbackCaption                              // Caption replaces the entire closed-caption screen, including clears.
)

// Feedback carries one observation. Kind identifies the populated field. Decoder
// diagnostics, raw command output, and media URLs must not be included.
type Feedback struct {
	Kind      FeedbackKind
	Position  float64
	Levels    AudioLevels
	Buffering bool
	Picture   PictureResult
	Caption   string
}

// PictureResult acknowledges one live request. Err leaves the preceding mode active.
type PictureResult struct {
	Request int
	Mode    PictureMode
	Err     error
}
