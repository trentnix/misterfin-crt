// Package branding supplies the project artwork embedded in every client build.
package branding

import (
	"bytes"
	_ "embed"
	"image"
	"image/draw"
	"image/png"
	"sync"
)

//go:embed mistervision.png
var logoPNG []byte

// Logo returns immutable project artwork, decoded once on first use. Callers
// must not modify it. Embedding keeps desktop and MiSTer installations identical.
func Logo() image.Image { return logo() }

var logo = sync.OnceValue(func() image.Image {
	im, err := png.Decode(bytes.NewReader(logoPNG))
	if err != nil {
		panic("invalid embedded project logo: " + err.Error())
	}
	rgba := image.NewRGBA(im.Bounds())
	draw.Draw(rgba, rgba.Bounds(), im, im.Bounds().Min, draw.Src)
	return rgba
})
