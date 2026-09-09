//go:build linux && cgo

package platform

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestPresentation(t *testing.T) {
	for _, tc := range []struct {
		spec           string
		g              Geometry
		bx, by, bw, bh int
	}{
		{"640x288", Geometry{640, 288, 640, 288}, 0, 0, 640, 288},
		{"640x240", Geometry{640, 240, 640, 240}, 0, 0, 640, 240},
		{"720x576", Geometry{720, 288, 720, 576}, 0, 0, 720, 576},
		{"720x480", Geometry{720, 240, 720, 480}, 0, 0, 720, 480},
		{"1280x720", Geometry{640, 288, 1280, 720}, 160, 0, 960, 720},
		{"640x360", Geometry{480, 240, 640, 360}, 80, 0, 480, 360},
		{"600x800", Geometry{600, 300, 600, 800}, 0, 175, 600, 450},
	} {
		t.Run(tc.spec, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "frame.raw")
			d, err := Open(Options{Headless: tc.spec, Output: path})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := d.Close(); err != nil {
					t.Error(err)
				}
			})
			if d.Geometry() != tc.g {
				t.Fatalf("geometry: %+v", d.Geometry())
			}
			pixels := make([]byte, tc.g.Width*tc.g.Height*4)
			for y := 0; y < tc.g.Height; y++ {
				for x := 0; x < tc.g.Width; x++ {
					i := (y*tc.g.Width + x) * 4
					pixels[i], pixels[i+1], pixels[i+2] = byte(x), byte(y), 127
				}
			}
			if err := d.Present(pixels); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(raw) != tc.g.OutputWidth*tc.g.OutputHeight*4 {
				t.Fatalf("dump size %d", len(raw))
			}
			for y := 0; y < tc.g.OutputHeight; y++ {
				for x := 0; x < tc.g.OutputWidth; x++ {
					want := []byte{0, 0, 0, 0}
					if x >= tc.bx && x < tc.bx+tc.bw && y >= tc.by && y < tc.by+tc.bh {
						sx, sy := (x-tc.bx)*tc.g.Width/tc.bw, (y-tc.by)*tc.g.Height/tc.bh
						i := (sy*tc.g.Width + sx) * 4
						want = pixels[i : i+4]
					}
					i := (y*tc.g.OutputWidth + x) * 4
					if !bytes.Equal(raw[i:i+4], want) {
						t.Fatalf("pixel %d,%d: got %v want %v", x, y, raw[i:i+4], want)
					}
				}
			}
			// Reuse the Go buffer across cgo calls, then close twice.
			clear(pixels)
			if err := d.Present(pixels); err != nil {
				t.Fatal(err)
			}
			raw, err = os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(raw, make([]byte, len(raw))) {
				t.Fatal("stale pixels after buffer reuse")
			}
			if err := d.Close(); err != nil {
				t.Fatal(err)
			}
			if err := d.Present(pixels); err == nil {
				t.Fatal("present after close succeeded")
			}
		})
	}
}

func TestErrors(t *testing.T) {
	for _, spec := range []string{"0x240", "640x-1", "640x288junk", "999999999999999x2", "8192x8192"} {
		if d, err := Open(Options{Headless: spec}); err == nil {
			d.Close()
			t.Errorf("accepted %q", spec)
		}
	}
	for _, options := range []Options{{Device: "/nonexistent/misterfin-fb"}, {Output: "frame.raw"}, {Headless: "1x1", Output: "bad\x00path"}} {
		if d, err := Open(options); err == nil {
			d.Close()
			t.Errorf("accepted %+v", options)
		}
	}
	d, err := Open(Options{Headless: "2x2", Output: filepath.Join(t.TempDir(), "missing", "frame.raw")})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, pixels := range [][]byte{nil, make([]byte, 15), make([]byte, 17), make([]byte, 16)} {
		if err := d.Present(pixels); err == nil {
			t.Errorf("expected error for %d bytes or unwritable dump", len(pixels))
		}
	}
}
