package testframe

import (
	"errors"
	"mistervision/internal/platform"
	"testing"
)

type recordingDisplay struct {
	pixels []byte
	err    error
}

func (*recordingDisplay) Geometry() platform.Geometry {
	return platform.Geometry{Width: 80, Height: 40, OutputWidth: 80, OutputHeight: 40}
}
func (d *recordingDisplay) Present(p []byte) error {
	d.pixels = append([]byte(nil), p...)
	return d.err
}
func (*recordingDisplay) Close() error { return nil }

func TestFrameWithoutC(t *testing.T) {
	d := &recordingDisplay{}
	if err := Present(d); err != nil {
		t.Fatal(err)
	}
	if len(d.pixels) != 80*40*4 {
		t.Fatal("wrong frame size")
	}
	// Interior red bar proves BGRX byte order.
	i := (10*80 + 55) * 4
	if got := d.pixels[i : i+4]; got[0] != 0 || got[1] != 0 || got[2] != 255 || got[3] != 0 {
		t.Fatalf("red bar: %v", got)
	}
	d.err = errors.New("display failed")
	if !errors.Is(Present(d), d.err) {
		t.Fatal("lost display error")
	}
}
