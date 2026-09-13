package browser

import (
	"image"

	"misterfin-go/internal/jellyfin"
)

// artUpdate delivers one independently completed image or metadata request.
// Cover slots preserve sample order even when requests finish out of order.
type artUpdate struct {
	kind        string
	image       image.Image
	detail      *jellyfin.Item
	count       *int
	slot, total int
	err         error
}

// applyArtwork updates a screen from a successful result. The browser loop is
// its only caller. Metadata and errors are handled separately by that loop.
func applyArtwork(art *Artwork, update artUpdate) {
	switch update.kind {
	case "Photo":
		art.Photo = update.image
	case "count":
		art.Count = update.count
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
