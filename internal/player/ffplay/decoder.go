// Package ffplay implements the ffplay command protocol.
package ffplay

import (
	"syscall"

	"misterfin-crt/internal/media"
	"misterfin-crt/internal/player"
)

// Decoder supplies desktop commands. FFplay has no slave control pipe,
// so pause and resume signal the isolated process group instead.
type Decoder struct {
	// Player overrides the executable. Empty resolves ffplay through PATH.
	Player string
	// Picture selects the initial recorded-video fit.
	Picture player.PictureMode
}

// Executable returns the configured executable or the implementation default.
func (d Decoder) Executable() string {
	if d.Player != "" {
		return d.Player
	}
	return "ffplay"
}

// Input selects descriptor 3 for both audio and video.
func (d Decoder) Input(media.Item) player.Input { return player.Pipe }

// Args builds FFplay arguments with timestamps rebased for this session.
// An empty source reads media from descriptor 3. Recorded video applies Picture.
func (d Decoder) Args(item media.Item, source string) []string {
	if source == "" {
		source = "pipe:3"
	}
	if item.Type == "Audio" {
		return []string{"-hide_banner", "-loglevel", "info", "-stats", "-autoexit", "-nodisp", "-vn", "-af", "asetpts=PTS-STARTPTS", "-i", source}
	}
	filter := "setpts=PTS-STARTPTS"
	if d.Picture.Zooms(item) {
		filter += ffplayZoomFilter(item)
	}
	return []string{"-hide_banner", "-loglevel", "info", "-stats", "-autoexit", "-exitonkeydown", "-window_title", "MiSTerFin CRT playback", "-vf", filter, "-af", "asetpts=PTS-STARTPTS", "-i", source}
}

// ffplayZoomFilter crops to 4:3 for wide and narrow sources. A source already
// near 4:3 receives a fixed 4/3 enlargement for baked-in letterboxing.
func ffplayZoomFilter(item media.Item) string {
	aspect := player.DisplayAspectRatio(item)
	switch {
	case aspect > player.DisplayAspect43+player.AspectTolerance:
		return ",crop=w='trunc(min(iw,ih*4/3/sar)/2)*2':h=ih"
	case aspect < player.DisplayAspect43-player.AspectTolerance:
		return ",crop=w=iw:h='trunc(min(ih,iw*sar*3/4)/2)*2'"
	default:
		return ",crop=w='trunc(iw*3/8)*2':h='trunc(ih*3/8)*2'"
	}
}

// Pause sends SIGSTOP when paused is true and SIGCONT otherwise.
// The signal applies to the isolated player process group.
func (d Decoder) Pause(c player.Control, paused bool) error {
	signal := syscall.SIGSTOP
	if !paused {
		signal = syscall.SIGCONT
	}
	return c.Signal(signal)
}

// Poll is a no-op because FFplay continuously reports progress.
func (d Decoder) Poll(player.Control) {}

// Refresh is a no-op because FFplay has no paused-frame refresh command.
func (d Decoder) Refresh(player.Control) {}

var _ player.Decoder = Decoder{}

// ClientSubtitles returns false because shared overlays do not reach the
// separate FFplay window. Subtitles must be included in its video stream.
func (d Decoder) ClientSubtitles() bool { return false }

// Validate returns nil because FFplay has no additional launch constraints.
// Executable lookup and media validation remain the caller's responsibility.
func (d Decoder) Validate(item media.Item) error {
	return nil
}

// Name identifies the protocol in diagnostics without exposing paths or arguments.
func (d Decoder) Name() string { return "ffplay" }

// WithPicture returns launch settings for one request without changing the receiver.
func (d Decoder) WithPicture(mode player.PictureMode) player.Decoder { d.Picture = mode; return d }
