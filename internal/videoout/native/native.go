// Package native presents UI and publishes video overlays on MiSTer.
package native

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"misterfin-crt/internal/platform"
	"misterfin-crt/internal/ui"
	"misterfin-crt/internal/videoout"
)

// OverlayPath is the shared publication path read by the patched MPlayer driver.
const OverlayPath = "/tmp/misterfin_crt_overlay"

var overlayMagic = [8]byte{'M', 'F', 'G', 'O', 'O', 'V', '1', 0}

// Backend implements videoout.Output.
type Backend struct {
	d         platform.Presenter
	path      string
	mu        sync.Mutex
	owners    int
	sequence  uint64
	published []byte
	handoff   framebufferHandoff
	physical  []byte // Scaled overlay, reused only while mu is held.
	payload   []byte // Cropped publication, independent of borrowed input pixels.
	loading   []byte // Black loading frame composited before decoder ownership.
}

// New publishes overlays for the patched MPlayer framebuffer driver.
func New(d platform.Presenter, path string) *Backend {
	return &Backend{d: d, path: path}
}

// Acquire registers a decoder and resets the first-frame handoff.
func (o *Backend) Acquire() {
	o.mu.Lock()
	if o.owners == 0 {
		o.handoff.claimed = false
	}
	o.owners++
	o.published = nil
	o.mu.Unlock()
}

// Release removes stale overlays after the last decoder relinquishes output.
func (o *Backend) Release() {
	o.mu.Lock()
	if o.owners > 0 {
		o.owners--
	}
	if o.owners == 0 {
		o.removeLocked()
	}
	o.mu.Unlock()
}

// Clear discards the last publication before a new item or browsing.
func (o *Backend) Clear() {
	o.mu.Lock()
	o.published = nil
	o.removeLocked()
	o.mu.Unlock()
}

// Close removes publications and releases handoff resources, not the display.
func (o *Backend) Close() error {
	o.Clear()
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.handoff.close()
}
func (o *Backend) removeLocked() {
	if err := os.Remove(o.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return
	}
}

// Geometry reports logical UI and physical framebuffer dimensions.
func (o *Backend) Geometry() platform.Geometry { return o.d.Geometry() }

// FrameInterval keeps browser motion at 60 Hz and overlay publication at 30 Hz.
// MPlayer presents video on its own clock independently of this interval.
func (o *Backend) FrameInterval(video bool) time.Duration {
	if video {
		return time.Second / 30
	}
	return time.Second / 60
}

// Present draws browsing or loading pixels and publishes changed video overlays.
func (o *Backend) Present(f videoout.Frame) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !f.Video {
		if o.published != nil {
			o.removeLocked()
		}
		return o.d.Present(f.UI)
	}
	overlay := f.Overlay
	if o.owners > 0 {
		if !bytes.Equal(o.published, overlay) {
			if err := o.publishLocked(overlay); err != nil {
				return err
			}
			o.published = append(o.published[:0], overlay...)
		}
		// A launched decoder may still be buffering. Keep drawing until its
		// first frame claims output, with no overlap between framebuffer writers.
		draw, err := o.handoff.begin(o.path + ".lock")
		if err != nil || !draw {
			return err
		}
		defer o.handoff.end()
	}
	if len(o.loading) != len(overlay) {
		o.loading = make([]byte, len(overlay))
	} else {
		clear(o.loading)
	}
	ui.Composite(o.loading, overlay)
	return o.d.Present(o.loading)
}

func (o *Backend) publishLocked(logical []byte) error {
	g := o.d.Geometry()
	if len(logical) != g.Width*g.Height*4 {
		return errors.New("video overlay must contain exactly logical width * height * 4 BGRA bytes")
	}
	o.physical = scale(o.physical, logical, g)
	physical := o.physical
	x, y, w, h := bounds(physical, g.OutputWidth, g.OutputHeight)
	if w == 0 || h == 0 {
		o.removeLocked()
		return nil
	}
	o.sequence++
	var header [40]byte
	copy(header[:], overlayMagic[:])
	values := []uint32{uint32(g.OutputWidth), uint32(g.OutputHeight), uint32(x), uint32(y), uint32(w), uint32(h)}
	for i, value := range values {
		binary.LittleEndian.PutUint32(header[8+i*4:], value)
	}
	binary.LittleEndian.PutUint64(header[32:], o.sequence)
	o.payload = slices.Grow(o.payload[:0], w*h*4)
	for yy := y; yy < y+h; yy++ {
		start := (yy*g.OutputWidth + x) * 4
		o.payload = append(o.payload, physical[start:start+w*4]...)
	}
	return atomicWrite(o.path, header[:], o.payload)
}

// scale reuses output storage and clears margins left by the previous geometry.
func scale(output, source []byte, g platform.Geometry) []byte {
	size := g.OutputWidth * g.OutputHeight * 4
	if len(output) != size {
		output = make([]byte, size)
	} else {
		clear(output)
	}
	bx, by, bw, bh := 0, 0, g.OutputWidth, g.OutputHeight
	lineDoubled := g.OutputWidth == g.Width && g.OutputHeight == g.Height*2
	if !lineDoubled && (g.OutputWidth != g.Width || g.OutputHeight != g.Height) {
		bw = g.OutputHeight * 4 / 3
		if bw > g.OutputWidth {
			bw = g.OutputWidth
			bh = g.OutputWidth * 3 / 4
		}
		bx, by = (g.OutputWidth-bw)/2, (g.OutputHeight-bh)/2
	}
	for y := 0; y < bh; y++ {
		sy := y * g.Height / bh
		for x := 0; x < bw; x++ {
			sx := x * g.Width / bw
			s, d := (sy*g.Width+sx)*4, ((by+y)*g.OutputWidth+bx+x)*4
			copy(output[d:d+4], source[s:s+4])
		}
	}
	return output
}

func bounds(pixels []byte, width, height int) (x, y, w, h int) {
	left, top, right, bottom := width, height, 0, 0
	for yy := 0; yy < height; yy++ {
		for xx := 0; xx < width; xx++ {
			if pixels[(yy*width+xx)*4+3] == 0 {
				continue
			}
			left, top, right, bottom = min(left, xx), min(top, yy), max(right, xx+1), max(bottom, yy+1)
		}
	}
	if right == 0 {
		return 0, 0, 0, 0
	}
	return left, top, right - left, bottom - top
}

func atomicWrite(path string, parts ...[]byte) (resultErr error) {
	dir := filepath.Dir(path)
	temporary, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create video overlay: %w", err)
	}
	name := temporary.Name()
	defer func() {
		if temporary != nil {
			resultErr = errors.Join(resultErr, temporary.Close())
		}
		_ = os.Remove(name)
	}()
	for _, part := range parts {
		if _, err := temporary.Write(part); err != nil {
			return fmt.Errorf("write video overlay: %w", err)
		}
	}
	if err := temporary.Close(); err != nil {
		temporary = nil
		return fmt.Errorf("close video overlay: %w", err)
	}
	temporary = nil
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("publish video overlay: %w", err)
	}
	return nil
}

var _ videoout.Output = (*Backend)(nil)
