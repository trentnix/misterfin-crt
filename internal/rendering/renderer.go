package rendering

import (
	"mistervision/internal/videoout"
)

// Renderer turns a read-only Scene into output-independent pixels. Call Render
// serially with positive logical dimensions. Returned Frame pixels are borrowed
// until the next Render call and must be presented or copied before then.
// A renderer owns its animation and caches. It performs no I/O or navigation.
type Renderer interface {
	Render(width, height int, scene Scene) videoout.Frame
}
