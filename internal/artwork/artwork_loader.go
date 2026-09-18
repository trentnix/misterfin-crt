package artwork

import (
	"context"
	"image"
	"sync"

	"mistervision/internal/media"
)

// Loader fetches and caches images for one authenticated session. It
// owns a three-request image limit across selections. Dimensions and client
// authentication must remain unchanged while workers are running.
type Loader struct {
	client                  media.Artwork
	photoWidth, photoHeight int
	slots                   chan struct{}
	cache                   artworkCache
	disk                    *DiskCache
	revisions               artworkRevisions
	mu                      sync.Mutex // Serializes retry with memory publication.
}

// remember excludes canceled requests and images superseded by an explicit retry.
func (l *Loader) remember(ctx context.Context, key imageKey, revision artworkRevision, im image.Image) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ctx.Err() != nil || l.revisions.current(key) != revision {
		return false
	}
	l.cache.remember(key, im)
	return true
}

// Forget invalidates memory and in-flight requests, including memory-only photos.
// Disk removal is deferred to image workers.
func (l *Loader) Forget(item media.Item) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cache.forget(item)
	for _, kind := range []string{"Primary", "Backdrop", "Logo", "Photo"} {
		l.revisions.invalidate(artworkKey(item, kind))
	}
}

// NewLoader binds one authenticated client, fixed photo dimensions, and optional
// disk storage before image workers start. Cached images are immutable.
func NewLoader(client media.Artwork, photoWidth, photoHeight int, disk *DiskCache) *Loader {
	return &Loader{
		client: client, photoWidth: photoWidth, photoHeight: photoHeight,
		slots: make(chan struct{}, 3), cache: newArtworkCache(), disk: disk,
	}
}

// Cached returns a decoded image without filesystem or network work. Nil is a miss.
func (l *Loader) Cached(item media.Item, kind string) image.Image {
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
		if l.cache.cached(key) == nil && l.revisions.current(key).number == 0 {
			l.cache.remember(key, cover.Image)
		}
	}
}

// artworkRevisions guards publication against Retry independently of storage.
// The mutex covers only revision changes and the final file rename, never image
// encoding, file writes, or pruning. The zero value is ready to use.
type artworkRevisions struct {
	mu        sync.Mutex
	revisions map[imageKey]artworkRevision
}

type artworkRevision struct {
	number  uint64
	discard bool
}

func (c *artworkRevisions) current(key imageKey) artworkRevision {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.revisions[key]
}

// invalidate marks the current tag for refresh, without filesystem work on input.
func (c *artworkRevisions) invalidate(key imageKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.revisions == nil {
		c.revisions = make(map[imageKey]artworkRevision)
	}
	r := c.revisions[key]
	r.number++
	r.discard = true
	c.revisions[key] = r
}
