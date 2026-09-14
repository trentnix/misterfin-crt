package playback

import (
	"errors"
	"fmt"
	"os/exec"

	"misterfin-crt/internal/jellyfin"
	playerapi "misterfin-crt/internal/player"
	"misterfin-crt/internal/player/ffplay"
	"misterfin-crt/internal/player/mplayer"
	"misterfin-crt/internal/player/pythonhelper"
)

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
func selectDecoder(o Config, item jellyfin.Item, picture PictureMode) (playerapi.Decoder, error) {
	if !Supported(item) {
		return nil, errors.New("playback for this item type is not implemented")
	}
	config := o.VideoDecoder
	if item.Type == "Audio" {
		config = o.AudioDecoder
	}
	var d playerapi.Decoder
	switch config.Kind {
	case DecoderMPlayer:
		d = mplayer.Decoder{Player: config.Player, Device: o.Device, Width: o.Width, Height: o.Height, Picture: picture}
	case DecoderFFplay:
		d = ffplay.Decoder{Player: config.Player, Picture: picture}
	case DecoderPython:
		if config.Player != "" {
			return nil, errors.New("Python playback requires a helper script and no player override")
		}
		d = pythonhelper.Decoder{Script: config.Helper, Output: o.FrameOutput, Width: o.Width, Height: o.Height, Picture: picture}
	default:
		return nil, errors.New("unknown decoder protocol")
	}
	if err := d.Validate(item); err != nil {
		return nil, err
	}
	return d, nil
}

// resolveDecoder locates the selected executable before playback preparation.
func resolveDecoder(o Config, item jellyfin.Item, picture PictureMode) (playerapi.Decoder, string, error) {
	d, err := selectDecoder(o, item, picture)
	if err != nil {
		return nil, "", err
	}
	executable, err := exec.LookPath(d.Executable())
	if err != nil {
		return nil, "", fmt.Errorf("player not found: %s", d.Executable())
	}
	return d, executable, nil
}
