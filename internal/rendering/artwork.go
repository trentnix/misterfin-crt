package rendering

import (
	"image"
)

// Artwork contains immutable decoded images.
// Renderers borrow these values and may retain the images for caching.
type Artwork struct {
	Primary, Backdrop, Logo, Photo image.Image
	Covers                         []image.Image
}
