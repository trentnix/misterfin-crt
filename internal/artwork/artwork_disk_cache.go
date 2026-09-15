package artwork

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"sync"
)

const artworkDiskBudget = 128 * 1024 * 1024
const artworkDiskEntries = 512

type artworkRevision struct {
	number  uint64
	discard bool
}

// DiskCache retains ordinary artwork across authenticated sessions.
// Workers serialize file I/O. Retry only changes revision state, so it does not
// wait for disk reads or writes. Older requests cannot overwrite a retried image.
type DiskCache struct {
	cacheFiles
	ioMu      sync.Mutex
	mu        sync.Mutex
	revisions map[imageKey]artworkRevision
}

// NewDiskCache partitions ordinary artwork by server and user. An empty root
// or identity disables persistence and returns nil.
func NewDiskCache(root, server, user string) *DiskCache {
	dir := accountCacheDir(root, server, user)
	if dir == "" {
		return nil
	}
	return &DiskCache{cacheFiles: cacheFiles{dir: dir, suffix: ".rgba", maxEntries: artworkDiskEntries, maxBytes: artworkDiskBudget}, revisions: make(map[imageKey]artworkRevision)}
}

func artworkFileName(key imageKey) string {
	data, _ := json.Marshal([]string{artworkMagic, key.id, key.kind, key.tag})
	return fmt.Sprintf("%x.rgba", sha256.Sum256(data))
}

func (c *DiskCache) revision(key imageKey) artworkRevision {
	if c == nil {
		return artworkRevision{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.revisions[key]
}

// invalidate marks the current tag for refresh, without filesystem work on input.
func (c *DiskCache) invalidate(key imageKey) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.revisions[key]
	r.number++
	r.discard = true
	c.revisions[key] = r
}

func (c *DiskCache) load(key imageKey, revision artworkRevision) image.Image {
	if c == nil {
		return nil
	}
	c.ioMu.Lock()
	defer c.ioMu.Unlock()
	if revision.discard {
		if c.revision(key) == revision {
			name := artworkFileName(key)
			c.remove(name)
		}
		return nil
	}
	data := c.read(artworkFileName(key), 20, artworkFileLimit)
	im := decodeCachedArtwork(data)
	if im == nil || c.revision(key) != revision {
		return nil
	}
	return im
}

// save uses atomic replacement and skips canceled or superseded requests.
// Cache failures do not prevent displaying an image fetched from Jellyfin.
func (c *DiskCache) save(ctx context.Context, key imageKey, revision artworkRevision, im image.Image) {
	if c == nil || ctx.Err() != nil {
		return
	}
	data := encodeArtwork(im)
	if data == nil {
		return
	}
	c.ioMu.Lock()
	defer c.ioMu.Unlock()
	if ctx.Err() != nil || c.revision(key) != revision {
		return
	}
	staged, err := c.stage(data)
	if err != nil {
		return
	}
	defer os.Remove(staged)
	if ctx.Err() != nil {
		return
	}
	name := artworkFileName(key)
	// The short commit step prevents invalidation racing publication. File
	// creation, pixel writes, and pruning never hold the revision mutex.
	c.mu.Lock()
	current := c.revisions[key]
	if current != revision || c.publish(staged, name) != nil {
		c.mu.Unlock()
		return
	}
	if current.discard {
		current.discard = false
		c.revisions[key] = current
	}
	c.mu.Unlock()
	c.written(name, len(data))
	c.prune(name)
}
