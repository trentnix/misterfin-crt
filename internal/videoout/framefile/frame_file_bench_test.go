package framefile

import (
	"os"
	"path/filepath"
	"testing"

	"misterfin-crt/internal/platform"
	"misterfin-crt/internal/videoout"
)

type benchmarkPresenter struct{}

func (benchmarkPresenter) Geometry() platform.Geometry {
	return platform.Geometry{Width: 640, Height: 240, OutputWidth: 640, OutputHeight: 480}
}
func (benchmarkPresenter) Present([]byte) error { return nil }

func BenchmarkUnchangedVideoFrame(b *testing.B) {
	p := filepath.Join(b.TempDir(), "frame")
	if err := os.WriteFile(p, make([]byte, 640*240*4), 0600); err != nil {
		b.Fatal(err)
	}
	o := New(benchmarkPresenter{}, p)
	f := videoout.Frame{Video: true, Overlay: make([]byte, 640*240*4)}
	if err := o.Present(f); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := o.Present(f); err != nil {
			b.Fatal(err)
		}
	}
}
