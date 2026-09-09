// Package platform defines the display boundary without requiring cgo.
package platform

// Geometry describes tightly packed BGRX8888 input and the physical output.
type Geometry struct {
	Width, Height, OutputWidth, OutputHeight int
}

// Display borrows pixels only during Present. Callers own the slice and must
// serialize all calls. Close releases resources and is safe to repeat.
type Display interface {
	Geometry() Geometry
	Present(pixels []byte) error
	Close() error
}

// Options selects hardware unless Headless is a nonempty WxH string.
// Output names an optional raw frame dump and is only valid in headless mode.
type Options struct {
	Device, Headless, Output string
}
