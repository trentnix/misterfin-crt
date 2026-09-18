package playback

import (
	"mistervision/internal/diagnostics"
	"mistervision/internal/player"
)

// Config holds reusable decoder and storage settings. Run copies Config and
// never writes to it. The caller owns Preferences and keeps it open until all
// playback calls return. Concurrent runs must use distinct decoder output
// destinations when a protocol writes to a shared path or device.
type Config struct {
	// Diagnostics is borrowed until playback and its detached cleanup finish.
	// Nil disables logging independently of the selected media server.
	Diagnostics *diagnostics.Log
	// Preferences remembers per-video choices. Nil disables persistence.
	Preferences *Preferences
	// VideoDecoder and AudioDecoder are immutable settings supplied by application
	// assembly. Run validates only the decoder for the requested media type.
	// A missing decoder fails before stream preparation. WithPicture makes the
	// per-request copy. Optional WithAudioLevels supplies per-process resources.
	VideoDecoder player.Decoder
	AudioDecoder player.Decoder
	// Timing supplies stream cadence independently of decoder or window dimensions.
	Timing Timing
}

// Timing supplies the target's server-side frame-rate policy. The zero value
// selects a 30 fps recorded-video and Live TV cap. Geometry stays in the decoder.
type Timing struct {
	// PAL selects the 25 fps recorded-video cap instead of 30 fps.
	PAL bool
	// LiveFrameRate overrides the Live TV cap when positive. Zero uses 25 fps
	// for PAL or 30 fps otherwise. Interlaced NTSC uses 30000/1001.
	LiveFrameRate float64
}

func (t Timing) liveFrameRate() float64 {
	if t.LiveFrameRate > 0 {
		return t.LiveFrameRate
	}
	if t.PAL {
		return 25
	}
	return 30
}
