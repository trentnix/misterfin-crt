package browser

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const artworkDiskBudget = 128 * 1024 * 1024
const artworkDiskEntries = 512

type artworkRevision struct {
	number  uint64
	discard bool
}

type artworkDiskEntry struct {
	size    int64
	written time.Time
}

// artworkDiskCache retains ordinary artwork across authenticated sessions.
// Workers serialize file I/O. Retry only changes revision state, so it does not
// wait for disk reads or writes. Older requests cannot overwrite a retried image.
type artworkDiskCache struct {
	dir       string
	ioMu      sync.Mutex
	mu        sync.Mutex
	revisions map[imageKey]artworkRevision
	files     map[string]artworkDiskEntry // Worker-owned inventory avoids rescanning per save.
}

func newArtworkDiskCache(root, server, user string) *artworkDiskCache {
	dir := accountCacheDir(root, server, user)
	if dir == "" {
		return nil
	}
	return &artworkDiskCache{dir: dir, revisions: make(map[imageKey]artworkRevision)}
}

func artworkFileName(key imageKey) string {
	data, _ := json.Marshal([]string{artworkMagic, key.id, key.kind, key.tag})
	return fmt.Sprintf("%x.rgba", sha256.Sum256(data))
}

func (c *artworkDiskCache) revision(key imageKey) artworkRevision {
	if c == nil {
		return artworkRevision{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.revisions[key]
}

// invalidate marks the current tag for refresh, without filesystem work on input.
func (c *artworkDiskCache) invalidate(key imageKey) {
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

func (c *artworkDiskCache) load(key imageKey, revision artworkRevision) image.Image {
	if c == nil {
		return nil
	}
	c.ioMu.Lock()
	defer c.ioMu.Unlock()
	if revision.discard {
		if c.revision(key) == revision {
			name := artworkFileName(key)
			if os.Remove(filepath.Join(c.dir, name)) == nil {
				delete(c.files, name)
			}
		}
		return nil
	}
	f, err := os.Open(filepath.Join(c.dir, artworkFileName(key)))
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 20 || info.Size() > artworkFileLimit {
		return nil
	}
	data := make([]byte, int(info.Size()))
	if _, err := io.ReadFull(f, data); err != nil {
		return nil
	}
	im := decodeCachedArtwork(data)
	if im == nil || c.revision(key) != revision {
		return nil
	}
	return im
}

// save uses atomic replacement and skips canceled or superseded requests.
// Cache failures do not prevent displaying an image fetched from Jellyfin.
func (c *artworkDiskCache) save(ctx context.Context, key imageKey, revision artworkRevision, im image.Image) {
	if c == nil || ctx.Err() != nil {
		return
	}
	data := encodeArtwork(im)
	if data == nil {
		return
	}
	c.ioMu.Lock()
	defer c.ioMu.Unlock()
	if ctx.Err() != nil || c.revision(key) != revision || os.MkdirAll(c.dir, 0700) != nil {
		return
	}
	f, err := os.CreateTemp(c.dir, ".artwork-")
	if err != nil {
		return
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil || closeErr != nil || ctx.Err() != nil {
		return
	}
	name := artworkFileName(key)
	// The short commit step prevents invalidation racing publication. File
	// creation, pixel writes, and pruning never hold the revision mutex.
	c.mu.Lock()
	current := c.revisions[key]
	if current != revision || os.Rename(f.Name(), filepath.Join(c.dir, name)) != nil {
		c.mu.Unlock()
		return
	}
	if current.discard {
		current.discard = false
		c.revisions[key] = current
	}
	c.mu.Unlock()
	if c.files != nil {
		c.files[name] = artworkDiskEntry{int64(len(data)), time.Now()}
	}
	c.prune(name)
}

// prune evicts oldest-written images. Hits cause no extra SD-card writes.
func (c *artworkDiskCache) prune(current string) {
	if c.files == nil {
		entries, err := os.ReadDir(c.dir)
		if err != nil {
			return
		}
		c.files = make(map[string]artworkDiskEntry)
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".rgba") {
				continue
			}
			info, err := entry.Info()
			if err == nil && info.Mode().IsRegular() {
				c.files[entry.Name()] = artworkDiskEntry{info.Size(), info.ModTime()}
			}
		}
	}
	var names []string
	var total int64
	for name, entry := range c.files {
		names = append(names, name)
		total += entry.size
	}
	if len(names) <= artworkDiskEntries && total <= artworkDiskBudget {
		return
	}
	sort.Slice(names, func(i, j int) bool { return c.files[names[i]].written.Before(c.files[names[j]].written) })
	count := len(names)
	for _, name := range names {
		if count <= artworkDiskEntries && total <= artworkDiskBudget {
			break
		}
		if name != current && os.Remove(filepath.Join(c.dir, name)) == nil {
			count--
			total -= c.files[name].size
			delete(c.files, name)
		}
	}
}
