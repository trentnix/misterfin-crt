package playback

import (
	"encoding/binary"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
)

// AudioLevels is a stereo RMS amplitude snapshot, normalized to [0,1].
// Missing samples and silence produce zero. It carries no rendering policy.
type AudioLevels [2]float64

// audioExport reads MPlayer's planar signed-16-bit export on the playback loop.
// The header's size is bytes across all channels, followed by a sample counter.
func audioExport(path string) AudioLevels {
	var levels AudioLevels
	f, err := os.Open(path)
	if err != nil {
		return levels
	}
	defer f.Close()
	var data [8208]byte
	n, _ := io.ReadFull(f, data[:])
	if n < 12 {
		return levels
	}
	channels := int(binary.LittleEndian.Uint32(data[:4]))
	size := int(binary.LittleEndian.Uint32(data[4:8]))
	if channels < 1 || channels > 8 || size < 2 || size > n-8 {
		return levels
	}
	samples := size / 2 / channels
	if samples == 0 {
		return levels
	}
	for ch := 0; ch < 2; ch++ {
		src := min(ch, channels-1)
		sum := 0.
		for i := 0; i < samples; i++ {
			offset := 8 + (src*samples+i)*2
			v := float64(int16(binary.LittleEndian.Uint16(data[offset:offset+2]))) / 32768
			sum += v * v
		}
		levels[ch] = math.Sqrt(sum / float64(samples))
	}
	return levels
}

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
