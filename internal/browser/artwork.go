package browser

import (
	"image"
)

// Artwork contains immutable decoded images and an optional library count.
// Renderers borrow these values and may retain the images for caching.
type Artwork struct {
	Primary, Backdrop, Logo, Photo image.Image
	Covers                         []image.Image
	Count                          *int
}
