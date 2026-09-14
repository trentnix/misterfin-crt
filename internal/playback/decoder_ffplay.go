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
		filter += ffplayZoomFilter(item)
	}
	return []string{"-hide_banner", "-loglevel", "info", "-stats", "-autoexit", "-exitonkeydown", "-window_title", "MiSTerFin CRT playback", "-vf", filter, "-af", "asetpts=PTS-STARTPTS", "-i", source}
}

// ffplayZoomFilter crops to 4:3 for wide and narrow sources. A source already
// near 4:3 receives a fixed 4/3 enlargement for baked-in letterboxing.
func ffplayZoomFilter(item jellyfin.Item) string {
	aspect := displayAspectRatio(item)
	switch {
	case aspect > displayAspect43+pictureAspectTolerance:
		return ",crop=w='trunc(min(iw,ih*4/3/sar)/2)*2':h=ih"
	case aspect < displayAspect43-pictureAspectTolerance:
		return ",crop=w=iw:h='trunc(min(ih,iw*sar*3/4)/2)*2'"
	default:
		return ",crop=w='trunc(iw*3/8)*2':h='trunc(ih*3/8)*2'"
	}
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
