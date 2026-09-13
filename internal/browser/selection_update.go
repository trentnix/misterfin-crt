package browser

import "misterfin-go/internal/jellyfin"

// selectionData is the selected item's presentation data. Library counts remain
// separate from images. The browser loop applies progressive results here.
type selectionData struct {
	artwork Artwork
	count   *int
}

// selectionUpdateKind distinguishes metadata from decoded image results.
type selectionUpdateKind uint8

const (
	selectionArtwork selectionUpdateKind = iota
	selectionDetails
	selectionCount
)

// selectionUpdate carries one progressive result. Only the payload named by
// kind is meaningful. err is the request error for every kind. Image workers may
// emit concurrently. Producers must not mutate data after publication.
type selectionUpdate struct {
	kind   selectionUpdateKind
	art    artUpdate
	detail *jellyfin.Item
	count  *int
	err    error
}
