package playback

import (
	"fmt"
	"strconv"

	"misterfin-go/internal/jellyfin"
)

// pythonDecoder speaks the helper's line protocol for inline video and audio.
// The helper decodes media and publishes clean frames. It does not build UX.
type pythonDecoder struct {
	script, output string
	width, height  int
}

func (d pythonDecoder) executable() string { return "python3" }

func (d pythonDecoder) input(item jellyfin.Item) decoderInput {
	if item.Type == "Audio" {
		return inputURL
	}
	return inputPipe
}

func (d pythonDecoder) args(item jellyfin.Item, source string) []string {
	var args []string
	if item.Type == "Audio" {
		args = []string{d.script, "--audio-only"}
	} else {
		args = []string{d.script, "--controls", "--status", "--output", d.output, "--width", strconv.Itoa(d.width), "--height", strconv.Itoa(d.height)}
	}
	if source != "" {
		args = append(args, "--source", source)
	}
	return args
}

func (d pythonDecoder) pause(c decoderControl, paused bool) error {
	_, err := fmt.Fprintf(c.stdin, "pause %t\n", paused)
	return err
}

// The helper pushes progress and leaves paused-frame composition to videoout.
func (d pythonDecoder) poll(decoderControl)    {}
func (d pythonDecoder) refresh(decoderControl) {}

var _ decoder = pythonDecoder{}
