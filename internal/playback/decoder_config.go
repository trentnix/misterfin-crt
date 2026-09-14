package playback

// DecoderKind identifies an executable's command and feedback protocol.
// The zero value is [DecoderMPlayer]. A protocol does not select input devices,
// storage paths, or a UI output backend.
type DecoderKind uint8

const (
	// DecoderMPlayer uses the MiSTer mplayer-arm slave protocol.
	DecoderMPlayer DecoderKind = iota
	// DecoderFFplay uses FFplay with process signals for pause and resume.
	DecoderFFplay
	// DecoderPython uses a Python helper with command and status pipes.
	DecoderPython
)

// DecoderConfig selects one audio or video decoder through [Config]. Run checks
// only the configuration for the requested media type. The zero value selects
// mplayer-arm at its default installation path.
type DecoderConfig struct {
	// Kind selects the executable protocol. Unknown values fail before playback
	// preparation starts.
	Kind DecoderKind
	// Player overrides the MPlayer or FFplay executable. Empty selects
	// /media/fat/misterfin-crt/mplayer-arm or ffplay, respectively. A bare name is
	// resolved through PATH. DecoderPython requires Player to be empty.
	Player string
	// Helper names the script required by DecoderPython, launched with python3
	// from PATH. MPlayer and FFplay ignore Helper.
	Helper string
}
