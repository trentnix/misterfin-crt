package browser

import (
	"context"
	"image"
	"sync"

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
	disk                    *artworkDiskCache
	mu                      sync.Mutex // Serializes retry with memory publication.
}

// remember excludes canceled requests and images superseded by an explicit retry.
func (l *artworkLoader) remember(ctx context.Context, key imageKey, disk *artworkDiskCache, revision artworkRevision, im image.Image) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ctx.Err() != nil || disk.revision(key) != revision {
		return false
	}
	l.cache.remember(key, im)
	return true
}

// forget invalidates memory immediately and defers disk removal to image workers.
func (l *artworkLoader) forget(item jellyfin.Item) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cache.forget(item)
	for _, kind := range []string{"Primary", "Backdrop", "Logo"} {
		l.disk.invalidate(artworkKey(item, kind))
	}
}

func newArtworkLoader(client *jellyfin.Client, photoWidth, photoHeight int) *artworkLoader {
	return &artworkLoader{
		client: client, photoWidth: photoWidth, photoHeight: photoHeight,
		slots: make(chan struct{}, 3), cache: newArtworkCache(),
	}
}
