package mplayer

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mistervision/internal/jellyfin"
	"mistervision/internal/player"
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
	if audioExport(path) != (player.AudioLevels{}) {
		t.Fatal("truncated export accepted")
	}
}

func TestUnavailableMeterReturnsNoResource(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	d, meter := (Decoder{}).WithAudioLevels()
	if meter != nil {
		meter.Close()
		t.Fatal("failed allocation returned an owned resource")
	}
	args := d.Args(jellyfin.Item{Type: "Audio"}, "")
	for _, arg := range args {
		if strings.Contains(arg, "export=") {
			t.Fatal("failed allocation enabled export")
		}
	}
}
