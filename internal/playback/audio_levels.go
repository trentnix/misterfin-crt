package playback

import (
	"math"
	"strconv"
	"strings"

	"misterfin-crt/internal/player"
)

// AudioLevels carries normalized stereo amplitude from the selected player.
type AudioLevels = player.AudioLevels

// parseAudioLevels accepts stereo ANS_AUDIO_LEVELS feedback only when both
// amplitudes are finite and within [0,1]. Malformed feedback returns false.
func parseAudioLevels(line string) (AudioLevels, bool) {
	var levels AudioLevels
	values := strings.Split(strings.TrimPrefix(line, "ANS_AUDIO_LEVELS="), ",")
	if !strings.HasPrefix(line, "ANS_AUDIO_LEVELS=") || len(values) != 2 {
		return levels, false
	}
	for i, value := range values {
		v, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
			return AudioLevels{}, false
		}
		levels[i] = v
	}
	return levels, true
}
