package playback

// Config holds reusable decoder and storage settings. Run copies Config and
// never writes to it. The caller owns Preferences and must keep it open until
// all playback calls using it return. Concurrent runs must use separate decoder
// output destinations if their selected protocol writes to a shared path/device.
type Config struct {
	// Preferences remembers per-video choices. Nil disables persistence.
	Preferences *Preferences
	// VideoDecoder selects the protocol for recorded video and Live TV.
	VideoDecoder DecoderConfig
	// AudioDecoder selects the protocol for Audio items independently of video.
	AudioDecoder DecoderConfig
	// FrameOutput is the complete path for Python video's clean BGRX frames.
	// The output backend must read that path. Playback adds no suffix. Audio
	// ignores this field. Python video requires a nonempty path.
	FrameOutput string
	// Device names the native framebuffer, normally /dev/fb0.
	Device string
	// Width and Height describe physical output pixels. MPlayer requires 640
	// pixels across and 240, 288, 480, or 576 rows. Python video requires 640x240
	// or 640x288. Height selects Jellyfin's NTSC (240/480) or PAL stream profile.
	Width, Height int
}
