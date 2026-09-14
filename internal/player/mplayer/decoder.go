// Package mplayer implements the patched MPlayer slave protocol for CRT output.
package mplayer

import (
	"errors"
	"fmt"
	"io"

	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/player"
)

// Decoder owns MiSTer's slave commands and CRT scaling policy. It holds
// configuration only. The caller owns the running child and pipes.
type Decoder struct {
	// Player overrides the executable. Empty uses the MiSTer installation path.
	Player string
	// Device names the framebuffer, normally /dev/fb0.
	Device string
	// Width and Height are physical output dimensions. Validate checks supported modes.
	Width, Height int
	// Picture selects the initial video fit.
	Picture player.PictureMode

	export string
}

// Executable returns the configured executable or the implementation default.
func (d Decoder) Executable() string {
	if d.Player != "" {
		return d.Player
	}
	return "/media/fat/misterfin-crt/mplayer-arm"
}

// Input selects the local proxy for seekable audio and descriptor 3 for video.
func (d Decoder) Input(item jellyfin.Item) player.Input {
	if item.Type == "Audio" {
		return player.URL
	}
	return player.Pipe
}

// Args builds audio filters or CRT video settings without opening resources.
// An empty source reads media from descriptor 3. Validate must succeed first.
func (d Decoder) Args(item jellyfin.Item, source string) []string {
	if source == "" {
		source = "/dev/fd/3"
	}
	if item.Type == "Audio" {
		filter := "volume=-3,lavcresample=48000"
		if d.export != "" {
			filter += ",export=" + d.export + ":512"
		}
		return []string{"-slave", "-quiet", "-nojoystick", "-noconsolecontrols", "-novideo", "-ao", "alsa", "-af", filter, source}
	}
	dar := player.DisplayAspectRatio(item)
	filter := fmt.Sprintf("misterfin=%d:%d:%.9f:%d", d.Width, d.Height, dar, d.Picture)

	// Match the C player's audio-clock correction. Recorded video smooths ALSA
	// delay measurements. Live TV reacts sooner to broadcast timing changes.
	autosync := "30"
	decodeOptions := "threads=2:fast"
	if jellyfin.IsLive(item) {
		autosync = "1"
		decodeOptions += ":misterfin-captions"
	}
	return []string{"-slave", "-quiet", "-nojoystick", "-noconsolecontrols", "-vo", "fbdev:" + d.Device, "-ao", "alsa", "-osdlevel", "0", "-framedrop", "-autosync", autosync, "-demuxer", "lavf", "-cache", "8192", "-cache-min", "20", "-sws", "0", "-vf", filter, "-lavdopts", decodeOptions, "-af", "volume=-3", source}
}

// Pause sends MPlayer's toggle command. The paused argument is not encoded.
// The caller must invoke Pause only when its desired pause state changes.
func (d Decoder) Pause(c player.Control, paused bool) error {
	_, err := io.WriteString(c.Stdin, "pause\n")
	return err
}

// Poll requests position feedback without changing the player's pause state.
func (d Decoder) Poll(c player.Control) {
	_, _ = io.WriteString(c.Stdin, "pausing_keep_force get_time_pos\n")
}

// Refresh requests a paused-frame redraw so MPlayer picks up overlay changes.
func (d Decoder) Refresh(c player.Control) {
	_, _ = io.WriteString(c.Stdin, "pausing_keep_force osd_show_text \" \" 1\n")
}

var _ player.Decoder = Decoder{}

// Seek moves audio by a signed offset in seconds while preserving pause state.
func (d Decoder) Seek(c player.Control, seconds int) error {
	_, err := fmt.Fprintf(c.Stdin, "pausing_keep seek %d 0\n", seconds)
	return err
}

// ClientSubtitles returns true because native output publishes shared overlays
// for the patched MPlayer to composite over video.
func (d Decoder) ClientSubtitles() bool { return true }

// WithAudioLevels gives this launch its own export file without changing the
// caller's decoder settings. Failure keeps playback available without meters.
func (d Decoder) WithAudioLevels() (player.Decoder, player.Meter) {
	meter := newAudioMeter()
	if meter == nil {
		return d, nil
	}
	d.export = meter.path
	return d, meter
}

// SetPicture requests a fit change and redraw of the current source frame.
// Position and pause state are preserved. MPlayer reports the result later
// through ANS_PICTURE_MODE, echoing request to identify the command.
func (d Decoder) SetPicture(c player.Control, mode player.PictureMode, request int) error {
	_, err := fmt.Fprintf(c.Stdin, "pausing_keep_force misterfin_picture %d %d\n", mode, request)
	return err
}

// Validate requires a 640-pixel framebuffer with 240, 288, 480, or 576 lines.
// It does not open the framebuffer or verify the installed player.
func (d Decoder) Validate(item jellyfin.Item) error {
	if d.Width != 640 || (d.Height != 240 && d.Height != 288 && d.Height != 480 && d.Height != 576) {
		return errors.New("MiSTer playback currently requires a 640-pixel PAL or NTSC framebuffer")
	}
	return nil
}

var (
	_ player.AudioSeeker     = Decoder{}
	_ player.PictureSetter   = Decoder{}
	_ player.LevelConfigurer = Decoder{}
)
