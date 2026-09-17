package player

import (
	"math"
	"strconv"
	"strings"

	"mistervision/internal/media"
)

// PictureMode controls how video fits the physical 4:3 display.
// Decoders apply the crop before the shared UI overlay is composed.
type PictureMode uint8

const (
	// PictureOriginal preserves the full picture and its display aspect ratio.
	PictureOriginal PictureMode = iota
	// PictureZoom43 enlarges the center of a picture and crops its
	// edges. A 4:3 source receives a fixed zoom for baked-in letterboxing.
	PictureZoom43
)

// DisplayAspect43 and AspectTolerance define the shared CRT fit policy.
// The tolerance accommodates rounded display-aspect metadata.
const (
	DisplayAspect43 = 4.0 / 3
	AspectTolerance = 0.01
)

// Zooms reports whether this mode requests a crop for video, including Live TV.
// Original 4:3 video can zoom to crop baked-in borders. Audio never zooms.
func (m PictureMode) Zooms(item media.Item) bool {
	return m == PictureZoom43 && item.Type != "Audio"
}

// DisplayAspectRatio uses the first video stream, preferring a valid a:b
// aspect ratio over encoded dimensions because pixels may not be square.
// Missing or out-of-range metadata falls back to 16:9.
func DisplayAspectRatio(item media.Item) float64 {
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
