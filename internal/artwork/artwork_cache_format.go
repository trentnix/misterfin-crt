package artwork

import (
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/draw"
)

const artworkMagic = "MFGOART1"
const artworkFileLimit = 16 + 640*360*4 + 4

// encodeArtwork preserves normalized RGBA pixels, including logo transparency.
// The version covers the ordinary image request sizes as well as this format.
// Photos have different request sizes and are not persisted here.
func encodeArtwork(im image.Image) []byte {
	if im == nil {
		return nil
	}
	b := im.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 || w > 640 || h > 360 {
		return nil
	}
	end := 16 + w*h*4
	data := make([]byte, end+4)
	rgba := &image.RGBA{Pix: data[16:end:end], Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
	draw.Draw(rgba, rgba.Bounds(), im, b.Min, draw.Src)
	copy(data, artworkMagic)
	binary.LittleEndian.PutUint32(data[8:], uint32(w))
	binary.LittleEndian.PutUint32(data[12:], uint32(h))
	binary.LittleEndian.PutUint32(data[end:], crc32.ChecksumIEEE(data[:end]))
	return data
}

// decodeCachedArtwork validates sizes and corruption, then transfers the read
// buffer's pixel storage to the image. The caller must relinquish data on success.
func decodeCachedArtwork(data []byte) *image.RGBA {
	if len(data) < 20 || len(data) > artworkFileLimit || string(data[:8]) != artworkMagic {
		return nil
	}
	w, h := binary.LittleEndian.Uint32(data[8:]), binary.LittleEndian.Uint32(data[12:])
	if w < 1 || h < 1 || w > 640 || h > 360 || len(data) != 20+int(w*h*4) {
		return nil
	}
	end := len(data) - 4
	if binary.LittleEndian.Uint32(data[end:]) != crc32.ChecksumIEEE(data[:end]) {
		return nil
	}
	return &image.RGBA{Pix: data[16:end:end], Stride: int(w) * 4, Rect: image.Rect(0, 0, int(w), int(h))}
}
