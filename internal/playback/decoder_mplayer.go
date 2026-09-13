package playback

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"misterfin-go/internal/jellyfin"
)

// mplayerDecoder owns MiSTer's slave commands and CRT scaling policy. It holds
// configuration only. The shared playerProcess owns the running child and pipes.
type mplayerDecoder struct {
	player, device string
	export         string
	width, height  int
}

func (d mplayerDecoder) executable() string {
	if d.player != "" {
		return d.player
	}
	return "/media/fat/misterfin-go/mplayer-arm"
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
	par := float64(d.width) * 3 / float64(d.height*4)
	w := d.width
	h := int(float64(w)/(dar*par) + 0.5)
	if h > d.height {
		h = d.height
		w = int(float64(h)*dar*par + 0.5)
	}
	filter := fmt.Sprintf("scale=%d:%d,expand=%d:%d,dsize=%d:%d", max(2, w/2*2), max(2, h/2*2), d.width, d.height, d.width, d.height)
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
