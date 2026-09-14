package sound

import (
	"encoding/binary"
	"errors"
)

// decodeClip validates embedded stereo 48 kHz signed-16-bit WAV data and scales
// it once at startup. Unsupported data must never be sent to the audio device.
func decodeClip(data []byte, volume int) ([]int16, error) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, errors.New("invalid sound WAV")
	}
	valid := false
	for offset := 12; offset+8 <= len(data); {
		size := int(binary.LittleEndian.Uint32(data[offset+4:]))
		start := offset + 8
		if size < 0 || size > len(data)-start {
			break
		}
		chunk := data[start : start+size]
		switch string(data[offset : offset+4]) {
		case "fmt ":
			valid = len(chunk) >= 16 && binary.LittleEndian.Uint16(chunk) == 1 && binary.LittleEndian.Uint16(chunk[2:]) == 2 && binary.LittleEndian.Uint32(chunk[4:]) == 48000 && binary.LittleEndian.Uint16(chunk[14:]) == 16
		case "data":
			if !valid || size == 0 || size%4 != 0 {
				return nil, errors.New("unsupported sound PCM")
			}
			pcm := make([]int16, size/2)
			for i := range pcm {
				pcm[i] = int16(binary.LittleEndian.Uint16(chunk[i*2:]))
			}
			// Convert through int16 before multiplying so negative samples keep their sign.
			for i := range pcm {
				pcm[i] = int16(int(pcm[i]) * volume / 100)
			}
			return pcm, nil
		}
		offset = start + size + (size & 1)
	}
	return nil, errors.New("missing sound PCM")
}
