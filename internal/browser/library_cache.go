package browser

import (
	"sync"
	"time"

	"misterfin-go/internal/jellyfin"
)

const libraryCacheLimit = 32
const libraryCacheTTL = time.Minute

type cachedLibrary struct {
	count      *int
	countUntil time.Time
	items      []jellyfin.Item
	itemsUntil time.Time
	used       uint64
}

// libraryCache owns library counts and cover sample metadata for one session.
// Methods synchronize access. Published counts and item slices are immutable.
// Counts and samples expire independently. Do not copy the cache after use.
type libraryCache struct {
	mu        sync.Mutex
	libraries map[string]cachedLibrary
	clock     uint64
}

func newLibraryCache() libraryCache {
	return libraryCache{libraries: make(map[string]cachedLibrary)}
}

// cached returns metadata and its separate count and sample deadlines.
// Callers check expiry. The returned count and item slice must not be mutated.
func (c *libraryCache) cached(id string) cachedLibrary {
	c.mu.Lock()
	defer c.mu.Unlock()
	value, ok := c.libraries[id]
	if ok {
		c.clock++
		value.used = c.clock
		c.libraries[id] = value
	}
	return value
}

// remember updates one metadata entry under the cache lock. The callback
// must not call other cache methods. Counts and cover samples expire separately.
func (c *libraryCache) remember(id string, update func(*cachedLibrary)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.libraries[id]; !ok && len(c.libraries) >= libraryCacheLimit {
		oldest := ""
		age := ^uint64(0)
		for k, v := range c.libraries {
			if v.used < age {
				oldest = k
				age = v.used
			}
		}
		delete(c.libraries, oldest)
	}
	value := c.libraries[id]
	update(&value)
	c.clock++
	value.used = c.clock
	c.libraries[id] = value
}

// forget invalidates both count and sample metadata for a retry.
func (c *libraryCache) forget(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.libraries, id)
}
