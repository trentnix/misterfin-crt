package artwork

import (
	"context"
	"image"
	"sync"

	"misterfin-crt/internal/jellyfin"
)

// Loader fetches and caches images for one authenticated session. It
// owns a three-request image limit across selections. Dimensions and client
// authentication must remain unchanged while workers are running.
type Loader struct {
	client                  *jellyfin.Client
	photoWidth, photoHeight int
	slots                   chan struct{}
	cache                   artworkCache
	disk                    *DiskCache
	mu                      sync.Mutex // Serializes retry with memory publication.
}

// remember excludes canceled requests and images superseded by an explicit retry.
func (l *Loader) remember(ctx context.Context, key imageKey, disk *DiskCache, revision artworkRevision, im image.Image) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ctx.Err() != nil || disk.revision(key) != revision {
		return false
	}
	l.cache.remember(key, im)
	return true
}

// Forget invalidates memory immediately and defers disk removal to image workers.
func (l *Loader) Forget(item jellyfin.Item) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cache.forget(item)
	for _, kind := range []string{"Primary", "Backdrop", "Logo"} {
		l.disk.invalidate(artworkKey(item, kind))
	}
}

// NewLoader binds one authenticated client, fixed photo dimensions, and optional
// disk storage before image workers start. Cached images are immutable.
func NewLoader(client *jellyfin.Client, photoWidth, photoHeight int, disk *DiskCache) *Loader {
	return &Loader{
		client: client, photoWidth: photoWidth, photoHeight: photoHeight,
		slots: make(chan struct{}, 3), cache: newArtworkCache(), disk: disk,
	}
}

// Cached returns a decoded image without filesystem or network work. Nil is a miss.
func (l *Loader) Cached(item jellyfin.Item, kind string) image.Image {
	return l.cache.cached(artworkKey(item, kind))
}

// Restore primes missing primary images from a saved mosaic. It does not replace
// newer cached images or mark library metadata fresh. Covers remain immutable.
func (l *Loader) Restore(ctx context.Context, covers []Cover) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, cover := range covers {
		if ctx.Err() != nil {
			return
		}
		key := imageKey{cover.ID, "Primary", cover.Tag}
		if l.cache.cached(key) == nil && l.disk.revision(key).number == 0 {
			l.cache.remember(key, cover.Image)
		}
	}
}
