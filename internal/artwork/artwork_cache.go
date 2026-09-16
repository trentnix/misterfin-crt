package artwork

import (
	"image"
	"sync"

	"misterfin-crt/internal/media"
)

const artworkBudget = 16 * 1024 * 1024

type imageKey struct{ id, kind, tag string }
type cachedImage struct {
	image image.Image
	bytes int
	used  uint64
}

// artworkCache owns decoded image retention for one authenticated session.
// Methods synchronize access. Cached images are immutable after publication.
// Do not copy the cache after its first use.
type artworkCache struct {
	mu     sync.Mutex
	images map[imageKey]cachedImage
	bytes  int
	clock  uint64
}

func newArtworkCache() artworkCache {
	return artworkCache{images: make(map[imageKey]cachedImage)}
}

// artworkKey includes the image tag so changed server artwork cannot reuse an
// older image. Parent backdrops share the parent identity. Photos use a distinct
// kind because their requested dimensions differ from ordinary primary artwork.
func artworkKey(item media.Item, kind string) imageKey {
	key := imageKey{item.ID, kind, item.ImageTags[kind]}
	if kind == "Photo" {
		key.tag = item.ImageTags["Primary"]
	}
	if kind == "Backdrop" {
		if len(item.BackdropImageTags) > 0 {
			key.tag = item.BackdropImageTags[0]
		} else if len(item.ParentBackdropImageTags) > 0 {
			key.id = item.ParentBackdropItemID
			key.tag = item.ParentBackdropImageTags[0]
		}
	}
	return key
}

// cached returns an immutable image and updates its eviction age. A miss is nil.
func (c *artworkCache) cached(key imageKey) image.Image {
	c.mu.Lock()
	defer c.mu.Unlock()
	value, ok := c.images[key]
	if !ok {
		return nil
	}
	c.clock++
	value.used = c.clock
	c.images[key] = value
	return value.image
}

// remember retains an immutable image within the 16 MiB and 128-entry limits.
// Replacement updates byte accounting. Least-recently-used images are evicted
// first. Nil images and images exceeding the entire budget are not retained.
func (c *artworkCache) remember(key imageKey, im image.Image) {
	if im == nil {
		return
	}
	size := im.Bounds().Dx() * im.Bounds().Dy() * 4
	if size > artworkBudget {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.images[key]; ok {
		c.bytes -= old.bytes
		delete(c.images, key)
	}
	for c.bytes+size > artworkBudget || len(c.images) >= 128 {
		var oldest imageKey
		age := ^uint64(0)
		for k, v := range c.images {
			if v.used < age {
				oldest = k
				age = v.used
			}
		}
		c.bytes -= c.images[oldest].bytes
		delete(c.images, oldest)
	}
	c.clock++
	c.images[key] = cachedImage{im, size, c.clock}
	c.bytes += size
}

// forget invalidates retry targets, including a shared parent backdrop, without
// evicting unrelated artwork. The caller owns metadata invalidation.
func (c *artworkCache) forget(item media.Item) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, value := range c.images {
		if key.id == item.ID || item.ParentBackdropItemID != "" && key.id == item.ParentBackdropItemID {
			c.bytes -= value.bytes
			delete(c.images, key)
		}
	}
}
