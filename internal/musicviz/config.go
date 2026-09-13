// Package musicviz draws music effects into the shared software canvas.
// Effects never open a decoder, read files, or select an output destination.
package musicviz

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config describes the default effect and the ordered selection cycle.
// A missing configuration uses Defaults. Assets are relative to the config file.
type Config struct {
	Default     string   `json:"default"`
	Meters      bool     `json:"meters"`
	Backgrounds []Preset `json:"backgrounds"`
}

// Preset configures a compiled effect or a user-supplied image animation.
// Type is none, starfield, rain, nebula, spinning, tunnel, sprites, or image.
type Preset struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Speed      float64  `json:"speed"`
	Density    int      `json:"density"`
	Intensity  *float64 `json:"intensity,omitempty"`
	Color      string   `json:"color,omitempty"`
	Files      []string `json:"files,omitempty"`
	FPS        float64  `json:"fps,omitempty"`
	frames     []image.Image
	groups     []Preset
	delays     []time.Duration
	color      uint32
	pending    bool
	toastyRoot string
}

// Library is immutable after loading and can be shared with the renderer.
type Library struct{ Config Config }

// Defaults supplies the built-in cycle. Toasty uses the existing C sprite assets.
func Defaults() Config {
	return Config{Default: "Starfield", Meters: true, Backgrounds: []Preset{
		{Name: "Starfield", Type: "starfield"}, {Name: "Rain", Type: "rain"},
		{Name: "Nebula", Type: "nebula"}, {Name: "Now Spinning", Type: "spinning"},
		{Name: "Tunnel", Type: "tunnel"}, {Name: "Toasty Squadron", Type: "sprites"},
		{Name: "Off", Type: "none"},
	}}
}

// Load validates configuration and decodes bounded assets off the render loop.
// Missing optional default sprites omit Toasty from the cycle. Explicit bad
// configuration or assets return an error so callers can report the failure.
func Load(path string) (*Library, error) { return load(path, true) }

// LoadPresets validates settings and locates assets without decoding them.
// The browser can publish this library immediately and load a selected asset later.
func LoadPresets(path string) (*Library, error) { return load(path, false) }

func load(path string, decode bool) (*Library, error) {
	config := Defaults()
	file, err := os.Open(path)
	var data []byte
	if err == nil {
		defer file.Close()
		data, err = io.ReadAll(io.LimitReader(file, (64<<10)+1))
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	explicit := err == nil
	if explicit {
		if len(data) > 64<<10 {
			return nil, fmt.Errorf("music configuration exceeds 64 KiB")
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err = dec.Decode(&config); err != nil {
			return nil, err
		}
		var extra any
		if dec.Decode(&extra) != io.EOF {
			return nil, fmt.Errorf("music configuration must contain one JSON object")
		}
	}
	if len(config.Backgrounds) == 0 || len(config.Backgrounds) > 16 {
		return nil, fmt.Errorf("music requires 1 to 16 backgrounds")
	}
	library := &Library{Config: config}
	library.Config.Backgrounds = nil
	budget := 32 << 20
	names := map[string]bool{}
	for _, p := range config.Backgrounds {
		if p.Name == "" || len(p.Name) > 32 || names[p.Name] {
			return nil, fmt.Errorf("music background names must be unique and 1 to 32 characters")
		}
		names[p.Name] = true
		if p.Speed == 0 {
			p.Speed = 1
		}
		if p.Density == 0 {
			p.Density = 40
		}
		if p.FPS == 0 {
			p.FPS = 12
		}
		if p.Speed < 0.1 || p.Speed > 4 || p.Density < 1 || p.Density > 128 || p.FPS < 1 || p.FPS > 60 {
			return nil, fmt.Errorf("music speed, density, or fps outside supported range")
		}
		if p.Intensity == nil {
			v := 0.65
			p.Intensity = &v
		}
		if *p.Intensity < 0 || *p.Intensity > 1 {
			return nil, fmt.Errorf("music intensity must be between 0 and 1")
		}
		p.color = 0x80bfff
		if p.Color != "" {
			n, e := strconv.ParseUint(strings.TrimPrefix(p.Color, "#"), 16, 24)
			if e != nil {
				return nil, fmt.Errorf("invalid music color")
			}
			p.color = uint32(n)
		}
		switch p.Type {
		case "none", "starfield", "rain", "nebula", "spinning", "tunnel":
		case "sprites", "image":
			files := p.Files
			if p.Type == "sprites" && len(files) == 0 {
				for _, root := range []string{filepath.Join(filepath.Dir(path), "assets", "toasty"), "assets/toasty", "/media/fat/misterfin/toasty"} {
					if _, e := os.Stat(filepath.Join(root, "asset1", "asset1_1.png")); e == nil {
						p.toastyRoot = root
						p.pending = !decode
						if decode {
							if err := p.loadToasty(root, &budget); err != nil {
								return nil, err
							}
						}
						break
					}
				}
				if p.toastyRoot == "" {
					if explicit {
						return nil, fmt.Errorf("Toasty sprite assets are missing")
					}
					continue
				}
				library.Config.Backgrounds = append(library.Config.Backgrounds, p)
				continue
			}

			if len(files) == 0 || len(files) > 128 {
				return nil, fmt.Errorf("%s requires 1 to 128 image files", p.Name)
			}
			p.Files = append([]string(nil), files...)
			for i, file := range files {
				if !filepath.IsAbs(file) {
					file = filepath.Join(filepath.Dir(path), file)
				}
				p.Files[i] = file
				if !decode {
					p.pending = true
					continue
				}
				if err := p.loadAsset(file, &budget); err != nil {
					return nil, fmt.Errorf("%s: %w", p.Name, err)
				}
			}
		default:
			return nil, fmt.Errorf("unknown music effect %q", p.Type)
		}
		library.Config.Backgrounds = append(library.Config.Backgrounds, p)
	}
	if library.Index(config.Default) < 0 {
		return nil, fmt.Errorf("default music background is not in cycle")
	}
	return library, nil
}

// Index returns a named preset's cycle position, or -1 when absent.
func (l *Library) Index(name string) int {
	for i, p := range l.Config.Backgrounds {
		if p.Name == name {
			return i
		}
	}
	return -1
}
