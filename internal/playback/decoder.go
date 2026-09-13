package playback

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"syscall"

	"misterfin-crt/internal/jellyfin"
)

// decoder defines the executable's command and control protocol. Implementations
// hold immutable launch configuration, not process or Jellyfin session state.
// args receives refreshed item metadata and an optional local proxy URL. Empty
// source means the shared process supplies media on file descriptor 3.
// Controls run serially on the playback loop. poll runs once per second.
// refresh is requested only while paused. Unsupported poll/refresh are no-ops.
type decoder interface {
	// clientSubtitles reports whether shared overlay text reaches the video.
	clientSubtitles() bool
	executable() string
	input(jellyfin.Item) decoderInput
	args(item jellyfin.Item, source string) []string
	pause(decoderControl, bool) error
	poll(decoderControl)
	refresh(decoderControl)
}

// audioSeeker is implemented by decoders with a controllable, seekable audio
// source. Video keeps its existing server-side stream replacement path.
type audioSeeker interface {
	seek(decoderControl, int) error
}

// decoderInput selects the source transport before the decoder starts.
// URL input uses the authenticated local proxy so audio can request byte ranges.
type decoderInput uint8

const (
	inputPipe decoderInput = iota
	inputURL
)

// decoderControl borrows the child process's stdin and process-group signaling.
// Implementations must not close stdin or retain either transport after a call.
type decoderControl struct {
	stdin  io.Writer
	signal func(syscall.Signal) error
}

// Supported reports whether the item type has a playback path. It does not
// verify stream availability, installed players, or decoder support.
func Supported(item jellyfin.Item) bool {
	if jellyfin.IsLive(item) {
		return true
	}
	switch item.Type {
	case "Movie", "Episode", "Video", "MusicVideo", "Audio":
		return true
	}
	return false
}

// selectDecoder validates the selected protocol without opening a process or
// making network requests. Audio and video settings are resolved by the caller.
func selectDecoder(o Options, item jellyfin.Item) (decoder, error) {
	if !Supported(item) {
		return nil, errors.New("playback for this item type is not implemented")
	}
	config := o.VideoDecoder
	if item.Type == "Audio" {
		config = o.AudioDecoder
	}
	picture := PictureOriginal
	if o.Tracks != nil {
		picture = o.Tracks.Picture
	}
	switch config.Kind {
	case DecoderMPlayer:
		if o.Width != 640 || (o.Height != 240 && o.Height != 288 && o.Height != 480 && o.Height != 576) {
			return nil, errors.New("MiSTer playback currently requires a 640-pixel PAL or NTSC framebuffer")
		}
		return mplayerDecoder{player: config.Player, device: o.Device, width: o.Width, height: o.Height, picture: picture}, nil
	case DecoderFFplay:
		return ffplayDecoder{player: config.Player, picture: picture}, nil
	case DecoderPython:
		if config.Helper == "" || config.Player != "" {
			return nil, errors.New("Python playback requires a helper script and no player override")
		}
		if item.Type != "Audio" && (o.FrameOutput == "" || o.Width != 640 || (o.Height != 240 && o.Height != 288)) {
			return nil, errors.New("Python video playback requires a frame output path and 640x240 or 640x288 geometry")
		}
		return pythonDecoder{script: config.Helper, output: o.FrameOutput, width: o.Width, height: o.Height, picture: picture}, nil
	default:
		return nil, errors.New("unknown decoder protocol")
	}
}

// resolveDecoder locates the selected executable before playback preparation.
func resolveDecoder(o Options, item jellyfin.Item) (decoder, string, error) {
	d, err := selectDecoder(o, item)
	if err != nil {
		return nil, "", err
	}
	executable, err := exec.LookPath(d.executable())
	if err != nil {
		return nil, "", fmt.Errorf("player not found: %s", d.executable())
	}
	return d, executable, nil
}
