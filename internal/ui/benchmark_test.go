package ui

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
)

func BenchmarkBackdrop(b *testing.B) {
	source := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	draw.Draw(source, source.Bounds(), image.NewUniform(color.RGBA{80, 120, 160, 255}), image.Point{}, draw.Src)
	canvas := New(640, 240)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		canvas.Blit(source, 0, 0, 640, 240)
		canvas.Shade(0, 0, 640, 240, 145)
	}
}
