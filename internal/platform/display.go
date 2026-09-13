// Package platform defines the display boundary without requiring cgo.
package platform

// Geometry describes tightly packed BGRX8888 input and the physical output.
type Geometry struct {
	Width, Height, OutputWidth, OutputHeight int
}

// Presenter borrows pixels during Present. Callers own the slice and serialize
// presentation calls. Output backends need this contract, not resource ownership.
type Presenter interface {
	// Geometry reports logical input dimensions and physical scanout dimensions.
	Geometry() Geometry
	// Present borrows exactly Width * Height * 4 bytes of tightly packed BGRX
	// pixels until it returns. It may block for display synchronization.
	Present(pixels []byte) error
}

// Display adds resource ownership to Presenter. The application that opens a
// display closes it after its output backend. Close is safe to repeat.
type Display interface {
	Presenter
	Close() error
}

// Options selects hardware unless Headless is a nonempty WxH string.
// Output names an optional raw frame dump and is only valid in headless mode.
type Options struct {
	Device, Headless, Output string
}
