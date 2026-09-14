package playback

import (
	"fmt"
	"io"

	"misterfin-crt/internal/jellyfin"
)

// mplayerDecoder owns MiSTer's slave commands and CRT scaling policy. It holds
// configuration only. The shared playerProcess owns the running child and pipes.
type mplayerDecoder struct {
	player, device string
	export         string
	width, height  int
	picture        PictureMode
}

func (d mplayerDecoder) executable() string {
	if d.player != "" {
		return d.player
	}
	return "/media/fat/misterfin-crt/mplayer-arm"
}

func (d mplayerDecoder) input(item jellyfin.Item) decoderInput {
	if item.Type == "Audio" {
		return inputURL
	}
	return inputPipe
}

func (d mplayerDecoder) args(item jellyfin.Item, source string) []string {
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
	dar := displayAspectRatio(item)
	par := float64(d.width) * 3 / float64(d.height*4)
	w := d.width
	h := int(float64(w)/(dar*par) + 0.5)
	if h > d.height {
		h = d.height
		w = int(float64(h)*dar*par + 0.5)
	}
	filter := fmt.Sprintf("scale=%d:%d,expand=%d:%d,dsize=%d:%d", max(2, w/2*2), max(2, h/2*2), d.width, d.height, d.width, d.height)
	if !jellyfin.IsLive(item) {
		filter = fmt.Sprintf("misterfin=%d:%d:%.9f:%d", d.width, d.height, dar, d.picture)
	}

	// Match the C player's audio-clock correction. Recorded video smooths ALSA
	// delay measurements. Live TV reacts sooner to broadcast timing changes.
	autosync := "30"
	if jellyfin.IsLive(item) {
		autosync = "1"
	}
	return []string{"-slave", "-quiet", "-nojoystick", "-noconsolecontrols", "-vo", "fbdev:" + d.device, "-ao", "alsa", "-osdlevel", "0", "-framedrop", "-autosync", autosync, "-demuxer", "lavf", "-cache", "8192", "-cache-min", "20", "-sws", "0", "-vf", filter, "-lavdopts", "threads=2:fast", "-af", "volume=-3", source}
}

func (d mplayerDecoder) pause(c decoderControl, paused bool) error {
	_, err := io.WriteString(c.stdin, "pause\n")
	return err
}

func (d mplayerDecoder) poll(c decoderControl) {
	_, _ = io.WriteString(c.stdin, "pausing_keep_force get_time_pos\n")
}

func (d mplayerDecoder) refresh(c decoderControl) {
	_, _ = io.WriteString(c.stdin, "pausing_keep_force osd_show_text \" \" 1\n")
}

var _ decoder = mplayerDecoder{}

// seek preserves pause state while moving within a direct-play audio source.
func (d mplayerDecoder) seek(c decoderControl, seconds int) error {
	_, err := fmt.Fprintf(c.stdin, "pausing_keep seek %d 0\n", seconds)
	return err
}

func (d mplayerDecoder) clientSubtitles() bool { return true }

// withAudioLevels gives this launch its own export file without changing the
// caller's decoder settings. Failure keeps playback available without meters.
func (d mplayerDecoder) withAudioLevels() (decoder, *audioMeter) {
	meter := newAudioMeter()
	if meter != nil {
		d.export = meter.path
	}
	return d, meter
}
