package playback

import (
	"errors"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"

	"misterfin-go/internal/jellyfin"
)

// Control requests an action on the active decoder. Unknown kinds are ignored.
type Control struct {
	// Kind is "pause" (toggle playback) or "refresh" (repaint paused native video).
	Kind string
}

// Options configures one call to [Run]. The caller must keep referenced values
// unchanged until Run returns. All callbacks are optional and run synchronously
// on Run's goroutine. They must return promptly and must not wait for Run to end.
type Options struct {
	// StartTicks overrides the saved video position in 100-nanosecond ticks.
	// Nil resumes normally. A pointer to zero restarts. Audio and Live TV ignore it.
	StartTicks *int64
	// Ready runs after the source opens, before waiting on Start. It does not
	// mean the decoder has started or displayed its first frame.
	Ready func()
	// Start gates decoder launch. Nil starts immediately. The caller closes
	// or sends on the channel to proceed. Context cancellation aborts the wait.
	Start <-chan struct{}
	// AsyncCleanup permits final progress reporting to outlive Run when this
	// channel can be received from at teardown. Nil keeps reporting synchronous.
	// The caller normally closes it during a seek handoff. Decoder and stream
	// cleanup still complete before Run returns.
	AsyncCleanup <-chan struct{}
	// AudioPlayer selects a Python audio helper when Player is empty.
	AudioPlayer string
	// Controls supplies decoder actions. Nil disables actions. Closing the
	// channel disables further actions without stopping playback.
	Controls <-chan Control
	// Paused reports a successfully issued pause or resume command, rather
	// than an acknowledgment that the decoder has completed the transition.
	Paused func(bool)
	// Buffering forwards explicit decoder buffering feedback when available.
	Buffering func(bool)
	// AcquireVideo runs after a video process starts, before stream copying.
	// ReleaseVideo pairs with it after process completion and stream-copy cleanup.
	// Audio invokes neither callback. ReleaseVideo requires AcquireVideo.
	AcquireVideo func()
	ReleaseVideo func()
	// Player overrides the default executable. Native playback defaults to
	// mplayer-arm. Headless playback defaults to FFplay.
	Player string
	// TerminalPlayer selects the Python inline decoder for headless video.
	// It requires FrameOutput and cannot be combined with Player.
	TerminalPlayer string
	// FrameOutput is the browser's raw frame path. The inline decoder publishes
	// clean video frames beside it with the ".video" suffix.
	FrameOutput string
	// Headless selects desktop player commands instead of native MiSTer commands.
	Headless bool
	// Device names the native framebuffer, normally /dev/fb0.
	Device string
	// Width and Height describe the physical output pixels, not UI layout pixels.
	Width, Height int
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
func (o Options) executable() string {
	if o.Player != "" {
		return o.Player
	}
	if o.TerminalPlayer != "" {
		return "python3"
	}
	if o.Headless {
		return "ffplay"
	}
	return "/media/fat/misterfin-go/mplayer-arm"
}
func (o Options) args(item jellyfin.Item) []string {
	if item.Type == "Audio" {
		if o.TerminalPlayer != "" {
			return []string{o.TerminalPlayer, "--audio-only"}
		}
		if o.Headless {
			return []string{"-hide_banner", "-loglevel", "info", "-stats", "-autoexit", "-nodisp", "-vn", "-af", "asetpts=PTS-STARTPTS", "-i", "pipe:3"}
		}
		return []string{"-slave", "-quiet", "-nojoystick", "-noconsolecontrols", "-novideo", "-ao", "alsa", "-af", "volume=-3,lavcresample=48000", "/dev/fd/3"}
	}
	if o.TerminalPlayer != "" {
		return []string{o.TerminalPlayer, "--controls", "--status", "--output", o.FrameOutput + ".video", "--width", strconv.Itoa(o.Width), "--height", strconv.Itoa(o.Height)}
	}
	if o.Headless {
		return []string{"-hide_banner", "-loglevel", "info", "-stats", "-autoexit", "-exitonkeydown", "-window_title", "MiSTerFin-Go playback", "-vf", "setpts=PTS-STARTPTS", "-af", "asetpts=PTS-STARTPTS", "-i", "pipe:3"}
	}
	// Match the C client's item_dar fallback for channels without video metadata.
	dar := 16.0 / 9
	for _, stream := range item.MediaStreams {
		if stream.Type == "Video" {
			if stream.Width > 0 && stream.Height > 0 {
				dar = float64(stream.Width) / float64(stream.Height)
			}
			parts := strings.Split(stream.AspectRatio, ":")
			if len(parts) == 2 {
				a, e1 := strconv.ParseFloat(parts[0], 64)
				b, e2 := strconv.ParseFloat(parts[1], 64)
				if e1 == nil && e2 == nil && a > 0 && b > 0 {
					dar = a / b
				}
			}
			break
		}
	}
	if math.IsNaN(dar) || math.IsInf(dar, 0) || dar < 0.1 || dar > 10 {
		dar = 16.0 / 9
	}
	par := float64(o.Width) * 3 / float64(o.Height*4)
	w := o.Width
	h := int(float64(w)/(dar*par) + 0.5)
	if h > o.Height {
		h = o.Height
		w = int(float64(h)*dar*par + 0.5)
	}
	filter := fmt.Sprintf("scale=%d:%d,expand=%d:%d,dsize=%d:%d", max(2, w/2*2), max(2, h/2*2), o.Width, o.Height, o.Width, o.Height)
	// Match the C player's audio-clock correction. Recorded video smooths ALSA
	// delay measurements. Live TV reacts sooner to broadcast timing changes.
	autosync := "30"
	if jellyfin.IsLive(item) {
		autosync = "1"
	}
	return []string{"-slave", "-quiet", "-nojoystick", "-noconsolecontrols", "-vo", "fbdev:" + o.Device, "-ao", "alsa", "-osdlevel", "0", "-framedrop", "-autosync", autosync, "-demuxer", "lavf", "-cache", "8192", "-cache-min", "20", "-sws", "0", "-vf", filter, "-lavdopts", "threads=2:fast", "-af", "volume=-3", "/dev/fd/3"}
}

// resolve validates the requested mode before locating its executable.
func (o Options) resolve(item jellyfin.Item) (Options, string, error) {
	if !Supported(item) {
		return o, "", errors.New("playback for this item type is not implemented")
	}
	if !o.Headless && (o.Width != 640 || (o.Height != 240 && o.Height != 288 && o.Height != 480 && o.Height != 576)) {
		return o, "", errors.New("MiSTer playback currently requires a 640-pixel PAL or NTSC framebuffer")
	}
	if o.TerminalPlayer != "" && (!o.Headless || o.FrameOutput == "" || o.Player != "" || o.Width != 640 || (o.Height != 240 && o.Height != 288)) {
		return o, "", errors.New("terminal playback requires 640x240 or 640x288 headless output and no player override")
	}
	if item.Type == "Audio" && o.AudioPlayer != "" && o.Player == "" {
		o.TerminalPlayer = o.AudioPlayer
	}
	executable, err := exec.LookPath(o.executable())
	if err != nil {
		return o, "", fmt.Errorf("player not found: %s", o.executable())
	}
	return o, executable, nil
}
