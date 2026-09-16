package bitmap

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestDecodeFitsWithoutEnlarging(t *testing.T) {
	for _, tc := range []struct {
		name                                                      string
		width, height, maxWidth, maxHeight, wantWidth, wantHeight int
	}{
		{"landscape", 8, 6, 4, 4, 4, 3},
		{"portrait", 6, 8, 4, 4, 3, 4},
		{"small", 2, 1, 8, 8, 2, 1},
		{"thin", 1, 9, 3, 2, 1, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			im, err := Decode(pngBytes(t, tc.width, tc.height), tc.maxWidth, tc.maxHeight)
			if err != nil {
				t.Fatal(err)
			}
			if im.Bounds().Dx() != tc.wantWidth || im.Bounds().Dy() != tc.wantHeight {
				t.Fatalf("bounds = %v", im.Bounds())
			}
		})
	}
}

func TestDecodePreservesSamplingAndTransparency(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	source.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 128})
	source.SetNRGBA(2, 0, color.NRGBA{B: 255, A: 255})
	var data bytes.Buffer
	if err := png.Encode(&data, source); err != nil {
		t.Fatal(err)
	}
	im, err := Decode(data.Bytes(), 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	for x := 0; x < 2; x++ {
		got := color.RGBAModel.Convert(im.At(x, 0))
		want := color.RGBAModel.Convert(source.At(x*2, 0))
		if got != want {
			t.Fatalf("pixel %d = %v, want %v", x, got, want)
		}
	}
}

func TestDecodeJPEG(t *testing.T) {
	var data bytes.Buffer
	if err := jpeg.Encode(&data, image.NewRGBA(image.Rect(0, 0, 8, 6)), nil); err != nil {
		t.Fatal(err)
	}
	im, err := Decode(data.Bytes(), 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	if im.Bounds() != image.Rect(0, 0, 4, 3) {
		t.Fatal(im.Bounds())
	}
}

func TestDecodeRejectsInvalidOrOversizedImages(t *testing.T) {
	valid := pngBytes(t, 2, 2)
	for name, data := range map[string][]byte{
		"not an image": []byte("not an image"),
		"truncated":    valid[:len(valid)-10],
		"too wide":     pngBytes(t, 2049, 1),
		"too tall":     pngBytes(t, 1, 2049),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(data, 640, 480); err == nil {
				t.Fatal("accepted invalid image")
			}
		})
	}
	for _, bounds := range [][2]int{{0, 1}, {1, 0}, {-1, 1}, {2049, 1}, {1, 2049}} {
		if _, err := Decode(valid, bounds[0], bounds[1]); err == nil {
			t.Fatal("accepted invalid output bounds", bounds)
		}
	}
}
