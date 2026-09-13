package playback

import (
	"syscall"

	"misterfin-crt/internal/jellyfin"
)

// ffplayDecoder supplies desktop commands. FFplay has no slave control pipe,
// so pause and resume signal the isolated process group instead.
type ffplayDecoder struct {
	player  string
	picture PictureMode
}

func (d ffplayDecoder) executable() string {
	if d.player != "" {
		return d.player
	}
	return "ffplay"
}

func (d ffplayDecoder) input(jellyfin.Item) decoderInput { return inputPipe }

func (d ffplayDecoder) args(item jellyfin.Item, source string) []string {
	if source == "" {
		source = "pipe:3"
	}
	if item.Type == "Audio" {
		return []string{"-hide_banner", "-loglevel", "info", "-stats", "-autoexit", "-nodisp", "-vn", "-af", "asetpts=PTS-STARTPTS", "-i", source}
	}
	filter := "setpts=PTS-STARTPTS"
	if d.picture.zooms(item) {
		// Account for anamorphic pixels. crop centers its output by default.
		filter += ",crop=w='min(iw,ih*4/3/sar)':h=ih"
	}
	return []string{"-hide_banner", "-loglevel", "info", "-stats", "-autoexit", "-exitonkeydown", "-window_title", "MiSTerFin CRT playback", "-vf", filter, "-af", "asetpts=PTS-STARTPTS", "-i", source}
}

func (d ffplayDecoder) pause(c decoderControl, paused bool) error {
	signal := syscall.SIGSTOP
	if !paused {
		signal = syscall.SIGCONT
	}
	return c.signal(signal)
}

// FFplay reports progress continuously and has no paused-frame refresh command.
func (d ffplayDecoder) poll(decoderControl)    {}
func (d ffplayDecoder) refresh(decoderControl) {}

var _ decoder = ffplayDecoder{}

func (d ffplayDecoder) clientSubtitles() bool { return false }
