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
	// Height is the physical output height used to select the server stream profile.
	// 240/480 select NTSC. Decoder geometry belongs to the injected implementation.
	Height int
}
