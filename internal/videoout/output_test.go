package videoout_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"misterfin-go/internal/platform"
	"misterfin-go/internal/videoout"
	"misterfin-go/internal/videoout/companion"
	"misterfin-go/internal/videoout/framefile"
	"misterfin-go/internal/videoout/native"
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

func TestCompanionCompositesOverlay(t *testing.T) {
	d := &testDisplay{geometry: platform.Geometry{Width: 1, Height: 1, OutputWidth: 1, OutputHeight: 1}}
	o := companion.New(d)
	if err := o.Present(videoout.Frame{UI: []byte{20, 40, 60, 0}, Overlay: []byte{100, 120, 140, 128}, Video: true}); err != nil {
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
	o := framefile.New(d, path)
	if err := o.Present(videoout.Frame{Overlay: []byte{100, 120, 140, 128}, Video: true}); err != nil {
		t.Fatal(err)
	}
	if want := []byte{60, 80, 100, 0}; !bytes.Equal(d.frame, want) {
		t.Fatalf("video composition: got %v want %v", d.frame, want)
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
	o := native.New(d, path)
	o.Acquire()
	overlay := make([]byte, 4*2*4)
	copy(overlay[4:8], []byte{10, 20, 30, 128})
	if err := o.Present(videoout.Frame{Overlay: overlay, Video: true}); err != nil {
		t.Fatal(err)
	}
	if d.frame != nil {
		t.Fatal("native backend wrote the display while the decoder owned it")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 40+2*4 {
		t.Fatalf("overlay size %d", len(data))
	}
	if string(data[:8]) != "MFGOOV1\x00" {
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

func TestBackendsPresentBrowserAndLoadingFrames(t *testing.T) {
	for _, name := range []string{"companion", "frameFile", "native"} {
		t.Run(name, func(t *testing.T) {
			d := &testDisplay{geometry: platform.Geometry{Width: 1, Height: 1, OutputWidth: 1, OutputHeight: 1}}
			path := filepath.Join(t.TempDir(), "output")
			var o videoout.Output
			switch name {
			case "companion":
				o = companion.New(d)
			case "frameFile":
				o = framefile.New(d, path)
			case "native":
				o = native.New(d, path)
			}
			t.Cleanup(func() { _ = o.Close() })
			if o.Geometry() != d.geometry {
				t.Fatal("backend changed display geometry")
			}
			browser := []byte{20, 40, 60, 0}
			presentBrowser := func() {
				t.Helper()
				if err := o.Present(videoout.Frame{UI: browser}); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(d.frame, browser) {
					t.Fatalf("browser frame: got %v want %v", d.frame, browser)
				}
			}
			presentBrowser()
			o.Clear()
			loading := videoout.Frame{UI: make([]byte, 4), Overlay: []byte{100, 120, 140, 128}, Video: true}
			presentLoading := func() {
				t.Helper()
				if err := o.Present(loading); err != nil {
					t.Fatal(err)
				}
				if want := []byte{50, 60, 70, 0}; !bytes.Equal(d.frame, want) {
					t.Fatalf("loading frame: got %v want %v", d.frame, want)
				}
			}
			presentLoading() // No decoder has acquired output yet.
			o.Acquire()
			if err := o.Present(loading); err != nil {
				t.Fatal(err)
			}
			o.Release()
			presentLoading() // Decoder handoff keeps the overlay visible.
			o.Clear()
			presentBrowser() // Exiting playback returns through the same entry point.
		})
	}
}

func TestFrameFileBrowserIgnoresStaleVideo(t *testing.T) {
	d := &testDisplay{geometry: platform.Geometry{Width: 1, Height: 1, OutputWidth: 1, OutputHeight: 1}}
	path := filepath.Join(t.TempDir(), "video.raw")
	if err := os.WriteFile(path, []byte{90, 90, 90, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	o := framefile.New(d, path)
	browser := []byte{20, 40, 60, 0}
	if err := o.Present(videoout.Frame{UI: browser}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(d.frame, browser) {
		t.Fatalf("stale decoder frame replaced browser: %v", d.frame)
	}
}
