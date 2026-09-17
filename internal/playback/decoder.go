package playback

import (
	"errors"
	"fmt"
	"os/exec"

	"mistervision/internal/media"
	playerapi "mistervision/internal/player"
)

// Supported reports whether the item type has a playback path. It does not
// verify stream availability, installed players, or decoder support.
func Supported(item media.Item) bool {
	if media.IsLive(item) {
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
func selectDecoder(o Config, item media.Item, picture PictureMode) (playerapi.Decoder, error) {
	if !Supported(item) {
		return nil, errors.New("playback for this item type is not implemented")
	}
	d := o.decoder(item)
	if d == nil {
		return nil, errors.New("no decoder configured for this media type")
	}
	d = d.WithPicture(picture)
	if err := d.Validate(item); err != nil {
		return nil, err
	}
	return d, nil
}

// resolveDecoder locates the selected executable before playback preparation.
func resolveDecoder(o Config, item media.Item, picture PictureMode) (playerapi.Decoder, string, error) {
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

// decoder selects injected settings without interpreting an executable protocol.
func (o Config) decoder(item media.Item) playerapi.Decoder {
	if item.Type == "Audio" {
		return o.AudioDecoder
	}
	return o.VideoDecoder
}
