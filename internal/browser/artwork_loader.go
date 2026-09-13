package browser

import (
	"misterfin-go/internal/jellyfin"
)

// artworkLoader fetches and caches images for one authenticated session. It
// owns a three-request image limit across selections. Dimensions and client
// authentication must remain unchanged while workers are running.
type artworkLoader struct {
	client                  *jellyfin.Client
	photoWidth, photoHeight int
	slots                   chan struct{}
	cache                   artworkCache
}

func newArtworkLoader(client *jellyfin.Client, photoWidth, photoHeight int) *artworkLoader {
	return &artworkLoader{
		client: client, photoWidth: photoWidth, photoHeight: photoHeight,
		slots: make(chan struct{}, 3), cache: newArtworkCache(),
	}
}
