package playback

// ControlKind identifies an internal request to the active decoder. These names
// are not decoder wire commands. Decoder implementations own their protocols.
type ControlKind string

const (
	// TogglePause switches the current pause state.
	TogglePause ControlKind = "pause"
	// SetPaused pauses without toggling an already paused decoder.
	SetPaused ControlKind = "set-pause"
	// Resume plays without toggling an already playing decoder.
	Resume ControlKind = "resume"
	// Report sends the current playback progress.
	Report ControlKind = "report"
	// Refresh redraws a paused video frame after an overlay change.
	Refresh ControlKind = "refresh"
	// SelectSubtitle selects client-rendered text and correlates its reply through Request.
	SelectSubtitle ControlKind = "subtitle"
	// SetPicture changes the live picture mode and correlates its reply through Request.
	SetPicture ControlKind = "picture"
	// SeekAudioStep accepts only a local ten-second step in either direction.
	SeekAudioStep ControlKind = "seek"
	// SeekAudioRelative carries a signed offset, despite its historical string
	// value. The browser converts a remote absolute target before sending it.
	SeekAudioRelative ControlKind = "seek-to"
)

// Control requests an action on the active decoder. Unsupported kinds report
// through Callbacks.ControlError without changing playback state.
type Control struct {
	Kind    ControlKind
	Request int         // Correlates subtitle or picture replies with the latest selection.
	Index   int         // Jellyfin stream index for a subtitle request.
	Seconds int         // Signed audio offset for either seek kind. Video replaces the stream.
	Picture PictureMode // Requested live picture mode.
}
