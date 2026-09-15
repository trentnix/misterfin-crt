package artwork

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const mosaicDiskEntries = 32
const mosaicDiskBudget = 64 * 1024 * 1024

// MosaicCache retains decoded collages across launches. It is separate from
// the C grid cache and partitioned by server and user, never by access token.
// Worker goroutines own its I/O. Failures are cache misses, not browser failures.
type MosaicCache struct {
	cacheFiles
	mu    sync.Mutex
	known map[string]uint32
}

// NewMosaicCache scopes a resolved cache directory to one server and user.
// An empty root or identity disables persistence and returns nil.
func NewMosaicCache(root, server, user string) *MosaicCache {
	dir := accountCacheDir(root, server, user)
	if dir == "" {
		return nil
	}
	return &MosaicCache{cacheFiles: cacheFiles{dir: dir, suffix: ".mosaic", maxEntries: mosaicDiskEntries, maxBytes: mosaicDiskBudget}, known: make(map[string]uint32)}
}

func mosaicFileName(id, collection string) string {
	return fmt.Sprintf("%x.mosaic", sha256.Sum256([]byte(id+"\x00"+collection)))
}

// Load restores a complete collage. Corrupt, missing, or disabled caches miss.
func (c *MosaicCache) Load(id, collection string) ([]Cover, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	name := mosaicFileName(id, collection)
	data := c.read(name, 16, mosaicFileLimit)
	covers, err := decodeMosaic(data)
	if err != nil {
		return nil, false
	}
	c.known[name] = binary.LittleEndian.Uint32(data[len(data)-4:])
	return covers, true
}

// Save publishes only complete, uncanceled collages. Identical content does not
// rewrite the SD card. Atomic rename prevents readers seeing partial files.
func (c *MosaicCache) Save(ctx context.Context, id, collection string, covers []Cover) {
	if c == nil || ctx.Err() != nil {
		return
	}
	data, err := encodeMosaic(covers)
	if err != nil || ctx.Err() != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	name := mosaicFileName(id, collection)
	path := filepath.Join(c.dir, name)
	sum := binary.LittleEndian.Uint32(data[len(data)-4:])
	if previous, ok := c.known[name]; ok && previous == sum {
		if _, err := os.Stat(path); err == nil {
			return
		}
	}
	staged, err := c.stage(data)
	if err != nil {
		return
	}
	defer os.Remove(staged)
	if ctx.Err() != nil || c.publish(staged, name) != nil {
		return
	}
	c.written(name, len(data))
	c.known[name] = sum
	c.prune(name)
}

// Forget removes a saved collage. Call it on a worker, not the browser loop.
func (c *MosaicCache) Forget(id, collection string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	name := mosaicFileName(id, collection)
	delete(c.known, name)
	c.remove(name)
}

// prune bounds each account to 32 collages and 64 MiB. Oldest written entries
// go first. Reads do not update timestamps or cause extra SD-card writes.
func (c *MosaicCache) prune(current string) {
	for _, name := range c.cacheFiles.prune(current) {
		delete(c.known, name)
	}
}
