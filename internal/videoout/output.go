// Package videoout owns the boundary between playback UI pixels and video output.
package videoout

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"misterfin-go/internal/platform"
	"misterfin-go/internal/ui"
	"os"
	"path/filepath"
	"sync"
)

const OverlayPath = "/tmp/misterfin_go_overlay"

var overlayMagic = [8]byte{'M', 'F', 'G', 'O', 'O', 'V', '1', 0}

// Output presents the browser's neutral video backdrop and BGRA overlay.
// Acquire and Release mark the period when an external decoder owns the display.
type Output interface {
	Start()
	Present(backdrop, overlay []byte) error
	Acquire()
	Release()
	Clear()
	Close() error
}

type companion struct {
	d platform.Display
}

// NewCompanion presents playback UI beside a player that owns another window.
func NewCompanion(d platform.Display) Output { return &companion{d: d} }

func (o *companion) Start()   {}
func (o *companion) Acquire() {}
func (o *companion) Release() {}
func (o *companion) Clear()   {}
func (o *companion) Close() error {
	return nil
}
func (o *companion) Present(backdrop, overlay []byte) error {
	frame := append([]byte(nil), backdrop...)
	ui.Composite(frame, overlay)
	return o.d.Present(frame)
}

type frameFile struct {
	d      platform.Display
	source string
}

// NewFrameFile composites UI over clean decoder frames published at source.
func NewFrameFile(d platform.Display, source string) Output {
	return &frameFile{d: d, source: source}
}

func (o *frameFile) Start()   { _ = os.Remove(o.source) }
func (o *frameFile) Acquire() {}
func (o *frameFile) Release() {}
func (o *frameFile) Clear()   { _ = os.Remove(o.source) }
func (o *frameFile) Close() error {
	o.Clear()
	return nil
}
func (o *frameFile) Present(_ []byte, overlay []byte) error {
	g := o.d.Geometry()
	frame, err := os.ReadFile(o.source)
	if err != nil || len(frame) != g.Width*g.Height*4 {
		frame = make([]byte, g.Width*g.Height*4)
	}
	ui.Composite(frame, overlay)
	return o.d.Present(frame)
}

type native struct {
	d         platform.Display
	path      string
	mu        sync.Mutex
	owners    int
	sequence  uint64
	published []byte
}

// NewNative publishes overlays for the patched MPlayer framebuffer driver.
func NewNative(d platform.Display, path string) Output {
	return &native{d: d, path: path}
}

func (o *native) Start() { o.Clear() }
func (o *native) Acquire() {
	o.mu.Lock()
	o.owners++
	o.published = nil
	o.mu.Unlock()
}
func (o *native) Release() {
	o.mu.Lock()
	if o.owners > 0 {
		o.owners--
	}
	if o.owners == 0 {
		o.removeLocked()
	}
	o.mu.Unlock()
}
func (o *native) Clear() {
	o.mu.Lock()
	o.published = nil
	o.removeLocked()
	o.mu.Unlock()
}
func (o *native) Close() error {
	o.Clear()
	return nil
}
func (o *native) removeLocked() {
	if err := os.Remove(o.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return
	}
}
func (o *native) Present(_ []byte, overlay []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.owners == 0 {
		frame := make([]byte, len(overlay))
		ui.Composite(frame, overlay)
		return o.d.Present(frame)
	}
	if bytes.Equal(o.published, overlay) {
		return nil
	}
	o.published = append(o.published[:0], overlay...)
	return o.publishLocked(overlay)
}

func (o *native) publishLocked(logical []byte) error {
	g := o.d.Geometry()
	if len(logical) != g.Width*g.Height*4 {
		return errors.New("video overlay must contain exactly logical width * height * 4 BGRA bytes")
	}
	physical := scale(logical, g)
	x, y, w, h := bounds(physical, g.OutputWidth, g.OutputHeight)
	if w == 0 || h == 0 {
		o.removeLocked()
		return nil
	}
	o.sequence++
	header := make([]byte, 40)
	copy(header, overlayMagic[:])
	values := []uint32{uint32(g.OutputWidth), uint32(g.OutputHeight), uint32(x), uint32(y), uint32(w), uint32(h)}
	for i, value := range values {
		binary.LittleEndian.PutUint32(header[8+i*4:], value)
	}
	binary.LittleEndian.PutUint64(header[32:], o.sequence)
	payload := make([]byte, 0, w*h*4)
	for yy := y; yy < y+h; yy++ {
		start := (yy*g.OutputWidth + x) * 4
		payload = append(payload, physical[start:start+w*4]...)
	}
	return atomicWrite(o.path, header, payload)
}

func scale(source []byte, g platform.Geometry) []byte {
	output := make([]byte, g.OutputWidth*g.OutputHeight*4)
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
