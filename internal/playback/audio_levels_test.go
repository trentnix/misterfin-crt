package playback

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestAudioExportStereoAndMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pcm")
	data := make([]byte, 8+16+8)
	binary.LittleEndian.PutUint32(data, 2)
	binary.LittleEndian.PutUint32(data[4:], 16)
	for i := 0; i < 8; i++ {
		v := uint16(16384)
		if i >= 4 {
			v = 8192
		}
		binary.LittleEndian.PutUint16(data[8+i*2:], v)
	}
	os.WriteFile(path, data, 0600)
	levels := audioExport(path)
	if math.Abs(levels[0]-.5) > .001 || math.Abs(levels[1]-.25) > .001 {
		t.Fatalf("bad RMS: %v", levels)
	}
	os.WriteFile(path, data[:10], 0600)
	if audioExport(path) != (AudioLevels{}) {
		t.Fatal("truncated export accepted")
	}
}
func TestParseAudioLevels(t *testing.T) {
	for _, line := range []string{"ANS_AUDIO_LEVELS=NaN,0", "ANS_AUDIO_LEVELS=Inf,0", "ANS_AUDIO_LEVELS=-1,0", "ANS_AUDIO_LEVELS=0,2", "ANS_AUDIO_LEVELS=0"} {
		if _, ok := parseAudioLevels(line); ok {
			t.Fatalf("accepted %q", line)
		}
	}
	if levels, ok := parseAudioLevels("ANS_AUDIO_LEVELS=0.5,0.1"); !ok || levels != (AudioLevels{.5, .1}) {
		t.Fatal(levels, ok)
	}
}
