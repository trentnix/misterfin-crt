package musicviz

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"time"
)

func (p *Preset) loadAsset(path string, budget *int) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 4<<20 {
		return fmt.Errorf("image exceeds 4 MiB")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return err
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 1024 || cfg.Height > 1024 {
		return fmt.Errorf("image dimensions exceed 1024 pixels")
	}
	if format == "gif" {
		if err = checkGIF(data, cfg.Width*cfg.Height); err != nil {
			return err
		}
		anim, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return err
		}
		canvas := image.NewRGBA(image.Rect(0, 0, cfg.Width, cfg.Height))
		for i, frame := range anim.Image {
			var previous *image.RGBA
			if len(anim.Disposal) > i && anim.Disposal[i] == gif.DisposalPrevious {
				previous = image.NewRGBA(canvas.Bounds())
				copy(previous.Pix, canvas.Pix)
			}
			draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
			if err = p.addFrame(canvas, time.Duration(max(2, anim.Delay[i]))*10*time.Millisecond, budget); err != nil {
				return err
			}
			if len(anim.Disposal) > i {
				switch anim.Disposal[i] {
				case gif.DisposalBackground:
					draw.Draw(canvas, frame.Bounds(), image.Transparent, image.Point{}, draw.Src)
				case gif.DisposalPrevious:
					canvas = previous
				}
			}
		}
		return nil
	}
	im, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	return p.addFrame(im, time.Duration(float64(time.Second)/p.FPS), budget)
}

func (p *Preset) addFrame(im image.Image, delay time.Duration, budget *int) error {
	b := im.Bounds()
	w, h := b.Dx(), b.Dy()
	limit := 640
	if p.Type == "sprites" {
		limit = 96
	}
	scale := min(1., float64(limit)/float64(max(w, h)))
	w = max(1, int(float64(w)*scale))
	h = max(1, int(float64(h)*scale))
	if len(p.frames) >= 128 || w*h*4 > *budget {
		return fmt.Errorf("music assets exceed 128 frames per preset or 32 MiB total")
	}
	*budget -= w * h * 4
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out.Set(x, y, im.At(b.Min.X+x*b.Dx()/w, b.Min.Y+y*b.Dy()/h))
		}
	}
	p.frames = append(p.frames, out)
	p.delays = append(p.delays, delay)
	return nil
}

// checkGIF bounds decompression before DecodeAll allocates every frame.
func checkGIF(b []byte, pixels int) error {
	bad := fmt.Errorf("GIF exceeds 128 frames or 32 MiB decoded, or is malformed")
	if len(b) < 13 {
		return bad
	}
	i := 13
	if b[10]&128 != 0 {
		i += 3 * (1 << ((b[10] & 7) + 1))
	}
	frames := 0
	blocks := func() bool {
		for i < len(b) {
			n := int(b[i])
			i++
			if n == 0 {
				return true
			}
			i += n
		}
		return false
	}
	for i < len(b) {
		kind := b[i]
		i++
		switch kind {
		case 0x3b:
			return nil
		case 0x21:
			i++
			if !blocks() {
				return bad
			}
		case 0x2c:
			if i+9 > len(b) {
				return bad
			}
			w := int(binary.LittleEndian.Uint16(b[i+4:]))
			h := int(binary.LittleEndian.Uint16(b[i+6:]))
			flags := b[i+8]
			i += 9
			frames++
			if w*h > pixels || frames > 128 || pixels*4*frames > 32<<20 {
				return bad
			}
			if flags&128 != 0 {
				i += 3 * (1 << ((flags & 7) + 1))
			}
			i++
			if !blocks() {
				return bad
			}
		default:
			return bad
		}
	}
	return bad
}

// loadToasty reuses each existing sprite sequence at a bounded render size.
func (p *Preset) loadToasty(root string, budget *int) error {
	for species := 1; species <= 15; species++ {
		group := Preset{Type: "sprites", FPS: p.FPS}
		for frame := 1; frame <= 128; frame++ {
			path := filepath.Join(root, fmt.Sprintf("asset%d", species), fmt.Sprintf("asset%d_%d.png", species, frame))
			if _, err := os.Stat(path); os.IsNotExist(err) {
				break
			}
			if err := group.loadAsset(path, budget); err != nil {
				return err
			}
		}
		if len(group.frames) > 0 {
			p.groups = append(p.groups, group)
		}
	}
	return nil
}

// Ready reports whether a preset can draw without further asset work.
func (l *Library) Ready(index int) bool {
	return index >= 0 && index < len(l.Config.Backgrounds) && !l.Config.Backgrounds[index].pending
}

// LoadAssets returns a new immutable library with one selected preset decoded.
// Existing frames remain shared. Call from a worker, never the render loop.
func (l *Library) LoadAssets(index int) (*Library, error) {
	if index < 0 || index >= len(l.Config.Backgrounds) {
		return nil, fmt.Errorf("invalid music preset")
	}
	if l.Ready(index) {
		return l, nil
	}
	budget := 32 << 20
	var used func(Preset) int
	used = func(p Preset) int {
		total := 0
		for _, frame := range p.frames {
			total += frame.Bounds().Dx() * frame.Bounds().Dy() * 4
		}
		for _, group := range p.groups {
			total += used(group)
		}
		return total
	}
	for _, p := range l.Config.Backgrounds {
		budget -= used(p)
	}
	next := &Library{Config: l.Config}
	next.Config.Backgrounds = append([]Preset(nil), l.Config.Backgrounds...)
	p := next.Config.Backgrounds[index]
	if p.toastyRoot != "" {
		if err := p.loadToasty(p.toastyRoot, &budget); err != nil {
			return nil, err
		}
	} else {
		for _, file := range p.Files {
			if err := p.loadAsset(file, &budget); err != nil {
				return nil, err
			}
		}
	}
	p.pending = false
	next.Config.Backgrounds[index] = p
	return next, nil
}
