package native

import (
	"path/filepath"
	"testing"

	"misterfin-crt/internal/platform"
)

type benchmarkPresenter struct{}

func (benchmarkPresenter) Geometry() platform.Geometry {
	return platform.Geometry{Width: 640, Height: 240, OutputWidth: 640, OutputHeight: 480}
}
func (benchmarkPresenter) Present([]byte) error { return nil }

func BenchmarkSmall480iOverlayPublication(b *testing.B) {
	o := New(benchmarkPresenter{}, filepath.Join(b.TempDir(), "overlay"))
	pixels := make([]byte, 640*240*4)
	for y := 100; y < 140; y++ {
		for x := 250; x < 390; x++ {
			pixels[(y*640+x)*4+3] = 64
		}
	}
	if err := o.publishLocked(pixels); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := o.publishLocked(pixels); err != nil {
			b.Fatal(err)
		}
	}
}
