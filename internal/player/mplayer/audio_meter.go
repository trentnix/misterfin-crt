package mplayer

import (
	"encoding/binary"
	"io"
	"math"
	"os"

	"mistervision/internal/player"
)

// audioMeter owns one MPlayer export file. The playback loop samples it while
// running. The caller removes it only after reaping the process that writes it.
type audioMeter struct{ path string }

// newAudioMeter allocates an export file for one launch. A nil result means
// sampling is unavailable. The caller must close a returned meter.
func newAudioMeter() *audioMeter {
	file, err := os.CreateTemp("", "mistervision-audio-*")
	if err != nil {
		return nil // Missing meters must not prevent music playback.
	}
	file.Close()
	return &audioMeter{path: file.Name()}
}

// Levels reads stereo RMS amplitudes from the export file. Missing or invalid
// data returns zero. Mono samples are repeated in both channels.
func (m *audioMeter) Levels() player.AudioLevels { return audioExport(m.path) }

// Close removes the export file after the decoder stops writing it.
// Calling Close again, or on a nil meter, is safe.
func (m *audioMeter) Close() {
	if m != nil {
		_ = os.Remove(m.path)
	}
}

// audioExport reads MPlayer's planar signed-16-bit export on the playback loop.
// The header's size is bytes across all channels, followed by a sample counter.
func audioExport(path string) player.AudioLevels {
	var levels player.AudioLevels
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
