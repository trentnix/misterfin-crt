// Package bitmap decodes bounded artwork independently of media servers,
// network requests, caches, and display output.
package bitmap

import (
	"bytes"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
)

// Decode returns an image that fits within maxWidth and maxHeight without
// enlarging it. Both bounds and source dimensions must be between 1 and 2048.
// Reduction uses nearest-neighbor sampling and preserves aspect ratio subject
// to pixel rounding. JPEG and PNG are registered here. Other formats require
// a decoder registered by the caller. The caller must bound encoded input size.
func Decode(b []byte, maxWidth, maxHeight int) (image.Image, error) {
	if maxWidth < 1 || maxHeight < 1 || maxWidth > 2048 || maxHeight > 2048 {
		return nil, errors.New("invalid artwork bounds")
	}
	conf, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || conf.Width < 1 || conf.Height < 1 || conf.Width > 2048 || conf.Height > 2048 {
		return nil, errors.New("invalid or oversized artwork")
	}
	im, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	scale := min(1.0, min(float64(maxWidth)/float64(conf.Width), float64(maxHeight)/float64(conf.Height)))
	if scale < 1 {
		resized := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(conf.Width)*scale)), max(1, int(float64(conf.Height)*scale))))
		bounds := im.Bounds()
		for y := 0; y < resized.Bounds().Dy(); y++ {
			for x := 0; x < resized.Bounds().Dx(); x++ {
				resized.Set(x, y, im.At(bounds.Min.X+x*bounds.Dx()/resized.Bounds().Dx(), bounds.Min.Y+y*bounds.Dy()/resized.Bounds().Dy()))
			}
		}
		im = resized
	}
	return im, nil
}
