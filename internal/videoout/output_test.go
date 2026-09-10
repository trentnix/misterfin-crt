package videoout

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"misterfin-go/internal/platform"
)

type testDisplay struct {
	geometry platform.Geometry
	frame    []byte
}

func (d *testDisplay) Geometry() platform.Geometry { return d.geometry }
func (d *testDisplay) Present(frame []byte) error {
	d.frame = append(d.frame[:0], frame...)
	return nil
}
func (d *testDisplay) Close() error { return nil }

func TestCompanionCompositesOverlay(t *testing.T) {
	d := &testDisplay{geometry: platform.Geometry{Width: 1, Height: 1, OutputWidth: 1, OutputHeight: 1}}
	o := NewCompanion(d)
	if err := o.Present([]byte{20, 40, 60, 0}, []byte{100, 120, 140, 128}); err != nil {
		t.Fatal(err)
	}
	want := []byte{60, 80, 100, 0}
	if !bytes.Equal(d.frame, want) {
		t.Fatalf("composited frame %v, want %v", d.frame, want)
	}
}

func TestFrameFileKeepsDecoderFrameClean(t *testing.T) {
	d := &testDisplay{geometry: platform.Geometry{Width: 1, Height: 1, OutputWidth: 1, OutputHeight: 1}}
	path := filepath.Join(t.TempDir(), "video.raw")
	source := []byte{20, 40, 60, 0}
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatal(err)
	}
	o := NewFrameFile(d, path)
	if err := o.Present(nil, []byte{100, 120, 140, 128}); err != nil {
		t.Fatal(err)
	}
	clean, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(clean, source) {
		t.Fatalf("decoder frame changed: %v", clean)
	}
}

func TestNativePublishesCroppedPhysicalOverlay(t *testing.T) {
	d := &testDisplay{geometry: platform.Geometry{Width: 4, Height: 2, OutputWidth: 4, OutputHeight: 4}}
	path := filepath.Join(t.TempDir(), "overlay")
	o := NewNative(d, path)
	o.Acquire()
	overlay := make([]byte, 4*2*4)
	copy(overlay[4:8], []byte{10, 20, 30, 128})
	if err := o.Present(nil, overlay); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 40+2*4 {
		t.Fatalf("overlay size %d", len(data))
	}
	if string(data[:8]) != string(overlayMagic[:]) {
		t.Fatalf("magic %q", data[:8])
	}
	values := []uint32{4, 4, 1, 0, 1, 2}
	for i, want := range values {
		if got := binary.LittleEndian.Uint32(data[8+i*4:]); got != want {
			t.Fatalf("header value %d: got %d want %d", i, got, want)
		}
	}
	if !bytes.Equal(data[40:], bytes.Repeat([]byte{10, 20, 30, 128}, 2)) {
		t.Fatalf("payload %v", data[40:])
	}
	o.Release()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("overlay remained after release: %v", err)
	}
}
