package browser

import (
	"image"

	"mistervision/internal/rendering"
)

// artUpdate delivers one independently completed image request.
// Cover slots preserve sample order even when requests finish out of order.
type artUpdate struct {
	kind        string
	image       image.Image
	slot, total int
	err         error
	covers      []image.Image // Complete sample snapshot, used only by "covers".
}

// applyArtwork updates a screen from a successful result. The browser loop is
// its only caller. Errors are handled separately by that loop.
func applyArtwork(art *rendering.Artwork, update artUpdate) {
	switch update.kind {
	case "covers":
		art.Covers = update.covers
	case "Photo":
		art.Photo = update.image
	case "Primary":
		art.Primary = update.image
	case "Backdrop":
		art.Backdrop = update.image
	case "Logo":
		art.Logo = update.image
	case "cover":
		if len(art.Covers) != update.total {
			art.Covers = make([]image.Image, update.total)
		}
		if update.slot >= 0 && update.slot < len(art.Covers) {
			art.Covers[update.slot] = update.image
		}
	}
}
