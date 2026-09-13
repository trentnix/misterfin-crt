package browser

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const mosaicDiskBudget = 64 * 1024 * 1024

// mosaicDiskCache retains decoded collages across launches. It is separate from
// the C grid cache and partitioned by server and user, never by access token.
// Worker goroutines own its I/O. Failures are cache misses, not browser failures.
type mosaicDiskCache struct {
	dir   string
	mu    sync.Mutex
	known map[string]uint32
}

// mosaicCacheRoot follows the C cache-root override while keeping Go files in
// their own subdirectory. Native defaults to the SD card, desktop to user cache.
func mosaicCacheRoot(headless bool) string {
	root := os.Getenv("MISTERFIN_CACHE_ROOT")
	if root == "" {
		root = "/media/fat"
		if headless {
			var err error
			root, err = os.UserCacheDir()
			if err != nil {
				return ""
			}
		}
	}
	return filepath.Join(root, "misterfin-go", "gridcache")
}

func newMosaicDiskCache(root, server, user string) *mosaicDiskCache {
	if root == "" || server == "" || user == "" {
		return nil
	}
	namespace := sha256.Sum256([]byte(strings.TrimRight(server, "/") + "\x00" + user))
	return &mosaicDiskCache{dir: filepath.Join(root, fmt.Sprintf("%x", namespace)), known: make(map[string]uint32)}
}

func mosaicFileName(id, collection string) string {
	return fmt.Sprintf("%x.mosaic", sha256.Sum256([]byte(id+"\x00"+collection)))
}

func (c *mosaicDiskCache) load(id, collection string) ([]mosaicCover, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	name := mosaicFileName(id, collection)
	f, err := os.Open(filepath.Join(c.dir, name))
	if err != nil {
		return nil, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > mosaicFileLimit {
		return nil, false
	}
	data := make([]byte, int(info.Size()))
	_, err = io.ReadFull(f, data)
	if err != nil {
		return nil, false
	}
	covers, err := decodeMosaic(data)
	if err != nil {
		return nil, false
	}
	c.known[name] = binary.LittleEndian.Uint32(data[len(data)-4:])
	return covers, true
}

// save publishes only complete, uncanceled collages. Identical content does not
// rewrite the SD card. Rename exposes a complete file without blocking readers.
func (c *mosaicDiskCache) save(ctx context.Context, id, collection string, covers []mosaicCover) {
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
	if os.MkdirAll(c.dir, 0700) != nil {
		return
	}
	f, err := os.CreateTemp(c.dir, ".mosaic-")
	if err != nil {
		return
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil || closeErr != nil || ctx.Err() != nil || os.Rename(f.Name(), path) != nil {
		return
	}
	c.known[name] = sum
	c.prune(name)
}

func (c *mosaicDiskCache) forget(id, collection string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	name := mosaicFileName(id, collection)
	delete(c.known, name)
	_ = os.Remove(filepath.Join(c.dir, name))
}

// prune bounds each account to 32 collages and 64 MiB. Oldest written entries
// go first. Reads do not update timestamps or cause extra SD-card writes.
func (c *mosaicDiskCache) prune(current string) {
	entries, _ := os.ReadDir(c.dir)
	var files []os.FileInfo
	var total int64
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".mosaic") {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.Mode().IsRegular() {
			files = append(files, info)
			total += info.Size()
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ModTime().Before(files[j].ModTime()) })
	count := len(files)
	for _, file := range files {
		if count <= libraryCacheLimit && total <= mosaicDiskBudget {
			break
		}
		if file.Name() != current && os.Remove(filepath.Join(c.dir, file.Name())) == nil {
			count--
			total -= file.Size()
			delete(c.known, file.Name())
		}
	}
}
