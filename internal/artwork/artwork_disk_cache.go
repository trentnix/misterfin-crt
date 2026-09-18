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

// DiskCache retains ordinary artwork across authenticated sessions.
// Workers serialize file I/O and use the loader's revisions to reject stale writes.
type DiskCache struct {
	cacheFiles
	ioMu sync.Mutex
}

// NewDiskCache partitions ordinary artwork by server and user. An empty root
// or identity disables persistence and returns nil.
func NewDiskCache(root, server, user string) *DiskCache {
	dir := accountCacheDir(root, server, user)
	if dir == "" {
		return nil
	}
	return &DiskCache{cacheFiles: cacheFiles{dir: dir, suffix: ".rgba", maxEntries: artworkDiskEntries, maxBytes: artworkDiskBudget}}
}

func artworkFileName(key imageKey) string {
	data, _ := json.Marshal([]string{artworkMagic, key.id, key.kind, key.tag})
	return fmt.Sprintf("%x.rgba", sha256.Sum256(data))
}

func (c *DiskCache) load(revisions *artworkRevisions, key imageKey, revision artworkRevision) image.Image {
	if c == nil {
		return nil
	}
	c.ioMu.Lock()
	defer c.ioMu.Unlock()
	if revision.discard {
		if revisions.current(key) == revision {
			name := artworkFileName(key)
			c.remove(name)
		}
		return nil
	}
	data := c.read(artworkFileName(key), 20, artworkFileLimit)
	im := decodeCachedArtwork(data)
	if im == nil || revisions.current(key) != revision {
		return nil
	}
	return im
}

// save uses atomic replacement and skips canceled or superseded requests.
// Cache failures do not prevent displaying an image fetched from the server.
func (c *DiskCache) save(ctx context.Context, revisions *artworkRevisions, key imageKey, revision artworkRevision, im image.Image) {
	if c == nil || ctx.Err() != nil {
		return
	}
	data := encodeArtwork(im)
	if data == nil {
		return
	}
	c.ioMu.Lock()
	defer c.ioMu.Unlock()
	if ctx.Err() != nil || revisions.current(key) != revision {
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
	revisions.mu.Lock()
	current := revisions.revisions[key]
	if current != revision || c.publish(staged, name) != nil {
		revisions.mu.Unlock()
		return
	}
	if current.discard {
		current.discard = false
		revisions.revisions[key] = current
	}
	revisions.mu.Unlock()
	c.written(name, len(data))
	c.prune(name)
}
