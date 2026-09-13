package playback

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"misterfin-crt/internal/jellyfin"
)

// PictureResult acknowledges one live request. Err leaves the preceding mode active.
type PictureResult struct {
	Request int
	Mode    PictureMode
	Err     error
}

// pictureSetter is optional. Other decoders keep the stream-handoff fallback.
type pictureSetter interface {
	setPicture(decoderControl, PictureMode, int) error
}

func (d mplayerDecoder) setPicture(c decoderControl, mode PictureMode, request int) error {
	_, err := fmt.Fprintf(c.stdin, "pausing_keep_force misterfin_picture %d %d\n", mode, request)
	return err
}

// PictureMode controls how recorded video fits the physical 4:3 display.
// Decoders apply the crop before the shared UI overlay is composed.
type PictureMode uint8

const (
	// PictureOriginal preserves the full picture and its display aspect ratio.
	PictureOriginal PictureMode = iota
	// PictureZoom43 fills the display with the center of a widescreen picture.
	// Pictures at or narrower than 4:3 keep their original fit.
	PictureZoom43
)

func (m PictureMode) zooms(item jellyfin.Item) bool {
	return m == PictureZoom43 && item.Type != "Audio" && !jellyfin.IsLive(item) && displayAspectRatio(item) > 4.0/3
}

// displayAspectRatio prefers display metadata over encoded dimensions, which
// may use non-square pixels. The fallback matches the C client's item_dar.
func displayAspectRatio(item jellyfin.Item) float64 {
	dar := 16.0 / 9
	for _, stream := range item.MediaStreams {
		if stream.Type != "Video" {
			continue
		}
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
	if math.IsNaN(dar) || math.IsInf(dar, 0) || dar < 0.1 || dar > 10 {
		return 16.0 / 9
	}
	return dar
}
