package playback

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"syscall"

	"misterfin-go/internal/jellyfin"
)

// decoder defines the executable's command and control protocol. Implementations
// hold immutable launch configuration, not process or Jellyfin session state.
// args receives refreshed item metadata and an optional local proxy URL. Empty
// source means the shared process supplies media on file descriptor 3.
// Controls run serially on the playback loop. poll runs once per second.
// refresh is requested only while paused. Unsupported poll/refresh are no-ops.
type decoder interface {
	executable() string
	input(jellyfin.Item) decoderInput
	args(item jellyfin.Item, source string) []string
	pause(decoderControl, bool) error
	poll(decoderControl)
	refresh(decoderControl)
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

// selectDecoder validates launch choices and resolves helper precedence without
// opening a process or making network requests. Player overrides keep the native
// or headless protocol. AudioPlayer overrides TerminalPlayer only for audio.
func selectDecoder(o Options, item jellyfin.Item) (decoder, error) {
	if !Supported(item) {
		return nil, errors.New("playback for this item type is not implemented")
	}
	if !o.Headless && (o.Width != 640 || (o.Height != 240 && o.Height != 288 && o.Height != 480 && o.Height != 576)) {
		return nil, errors.New("MiSTer playback currently requires a 640-pixel PAL or NTSC framebuffer")
	}
	if o.TerminalPlayer != "" && (!o.Headless || o.FrameOutput == "" || o.Player != "" || o.Width != 640 || (o.Height != 240 && o.Height != 288)) {
		return nil, errors.New("terminal playback requires 640x240 or 640x288 headless output and no player override")
	}
	script := o.TerminalPlayer
	if item.Type == "Audio" && o.AudioPlayer != "" && o.Player == "" {
		script = o.AudioPlayer
	}
	if script != "" {
		return pythonDecoder{script: script, output: o.FrameOutput + ".video", width: o.Width, height: o.Height}, nil
	}
	if o.Headless {
		return ffplayDecoder{player: o.Player}, nil
	}
	return mplayerDecoder{player: o.Player, device: o.Device, width: o.Width, height: o.Height}, nil
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
