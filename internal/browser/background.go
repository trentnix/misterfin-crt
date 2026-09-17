package browser

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"

	"mistervision/internal/settings"
)

// LoadBackground reads a legacy optional background.json and its image.
func LoadBackground(path string) (image.Image, error) {
	return ParseBackground(settings.Read(path, 4096, false))
}

// ParseBackground decodes the background section's image once at startup.
// Paths resolve beside the settings file. Nil preserves normal artwork.
// Invalid settings or assets return errors for caller-controlled fallback.
func ParseBackground(source settings.Section) (image.Image, error) {
	var config struct {
		Image string `json:"image"`
	}
	if err := source.Decode(&config); err != nil {
		return nil, err
	}
	if config.Image == "" {
		return nil, nil
	}
	name := config.Image
	if !filepath.IsAbs(name) {
		name = filepath.Join(filepath.Dir(source.Path), name)
	}
	return loadBackgroundImage(name)
}

// loadBackgroundImage bounds encoded and decoded size before allocating pixels.
func loadBackgroundImage(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("background image: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 4<<20 {
		return nil, fmt.Errorf("background image exceeds 4 MiB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") {
		return nil, fmt.Errorf("background image must be PNG or JPEG")
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 2048 || config.Height > 2048 {
		return nil, fmt.Errorf("background image dimensions must be between 1 and 2048 pixels")
	}
	im, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("cannot decode background image")
	}
	// Normalize paletted PNGs and JPEGs once so cached drawing uses the fast
	// RGBA path and compares a stable image identity.
	if _, ok := im.(*image.RGBA); !ok {
		b := im.Bounds()
		rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(rgba, rgba.Bounds(), im, b.Min, draw.Src)
		im = rgba
	}
	return im, nil
}
