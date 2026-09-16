// Package pythonhelper implements the command protocol used by the libmpv Python helper.
package pythonhelper

import (
	"errors"
	"fmt"
	"strconv"

	"misterfin-crt/internal/media"
	"misterfin-crt/internal/player"
)

// Decoder speaks the helper's line protocol for inline video and audio.
// The helper decodes media and publishes clean frames. It does not build UX.
type Decoder struct {
	// Script names a helper implementing this package's command protocol.
	Script string
	// Output is the clean BGRX frame destination for video.
	Output string
	// Width and Height describe the logical CRT video frame.
	Width, Height int
	// Picture selects the initial video fit.
	Picture player.PictureMode

	levels bool
}

// Executable returns python3, which the caller resolves through PATH.
func (d Decoder) Executable() string { return "python3" }

// Input selects the local proxy for seekable audio and descriptor 3 for video.
func (d Decoder) Input(item media.Item) player.Input {
	if item.Type == "Audio" {
		return player.URL
	}
	return player.Pipe
}

// Args builds helper arguments for audio or clean video frame output.
// An empty source uses the helper's descriptor 3 default. Validate must succeed first.
func (d Decoder) Args(item media.Item, source string) []string {
	var args []string
	if item.Type == "Audio" {
		args = []string{d.Script, "--audio-only"}
		if d.levels {
			args = append(args, "--audio-levels")
		}
	} else {
		args = []string{d.Script, "--controls", "--status", "--output", d.Output, "--width", strconv.Itoa(d.Width), "--height", strconv.Itoa(d.Height)}
		if media.IsLive(item) {
			args = append(args, "--captions")
		}
		if d.Picture.Zooms(item) {
			args = append(args, "--zoom-4-3")
		}
	}
	if source != "" {
		args = append(args, "--source", source)
	}
	return args
}

// Pause sends an explicit pause state through the helper's command pipe.
func (d Decoder) Pause(c player.Control, paused bool) error {
	_, err := fmt.Fprintf(c.Stdin, "pause %t\n", paused)
	return err
}

// Poll is a no-op because the helper pushes progress.
func (d Decoder) Poll(player.Control) {}

// Refresh is a no-op because the output backend composes paused overlays.
func (d Decoder) Refresh(player.Control) {}

var _ player.Decoder = Decoder{}

// Seek moves audio by a signed offset in seconds while preserving pause state.
func (d Decoder) Seek(c player.Control, seconds int) error {
	_, err := fmt.Fprintf(c.Stdin, "seek %d\n", seconds)
	return err
}

// SetPicture changes the render fit without replacing the decoder or its source.
// The helper preserves pause state and later acknowledges through
// ANS_PICTURE_MODE, echoing request to identify the command.
func (d Decoder) SetPicture(c player.Control, mode player.PictureMode, request int) error {
	_, err := fmt.Fprintf(c.Stdin, "picture %d %d\n", mode, request)
	return err
}

// ClientSubtitles returns true because the output backend composites shared
// subtitle overlays onto the helper's clean video frames.
func (d Decoder) ClientSubtitles() bool { return true }

// WithAudioLevels returns a copy configured for audio feedback on the status
// pipe. The returned meter is nil because no export file is needed.
func (d Decoder) WithAudioLevels() (player.Decoder, player.Meter) {
	d.levels = true
	return d, nil
}

// Validate requires a helper script. Video also requires an output path and
// 640x240 or 640x288 geometry. It does not check whether the script exists.
func (d Decoder) Validate(item media.Item) error {
	if d.Script == "" {
		return errors.New("Python playback requires a helper script and no player override")
	}
	if item.Type != "Audio" && (d.Output == "" || d.Width != 640 || (d.Height != 240 && d.Height != 288)) {
		return errors.New("Python video playback requires a frame output path and 640x240 or 640x288 geometry")
	}
	return nil
}

var (
	_ player.AudioSeeker     = Decoder{}
	_ player.PictureSetter   = Decoder{}
	_ player.LevelConfigurer = Decoder{}
)

// Name identifies the protocol in diagnostics without exposing paths or arguments.
func (d Decoder) Name() string { return "python" }

// WithPicture returns launch settings for one request without changing the receiver.
func (d Decoder) WithPicture(mode player.PictureMode) player.Decoder { d.Picture = mode; return d }
