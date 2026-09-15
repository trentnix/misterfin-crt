package artwork

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type cacheEntry struct {
	size    int64
	written time.Time
}

// cacheFiles shares file mechanics without deciding image freshness. Its owner
// serializes I/O and guards publication. Only successful writes update inventory.
// One scan per instance accounts for existing files. Concurrent application
// processes sharing a directory are not coordinated by the in-memory inventory.
type cacheFiles struct {
	dir, suffix string
	maxEntries  int
	maxBytes    int64
	files       map[string]cacheEntry
}

// read accepts only regular files within bounds before allocating a read buffer.
func (c *cacheFiles) read(name string, minimum, maximum int64) []byte {
	f, err := os.Open(filepath.Join(c.dir, name))
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < minimum || info.Size() > maximum {
		return nil
	}
	data := make([]byte, int(info.Size()))
	if _, err := io.ReadFull(f, data); err != nil {
		return nil
	}
	return data
}

// stage writes and closes a private file on the destination filesystem. The
// caller must remove its path after publishing or abandoning it. Cache entries
// are rebuildable, so these writes intentionally do not require fsync.
func (c *cacheFiles) stage(data []byte) (string, error) {
	if err := os.MkdirAll(c.dir, 0700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(c.dir, ".cache-")
	if err != nil {
		return "", err
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// publish performs only the atomic rename so callers can hold a short-lived
// revision guard without holding it through pixel writes or budget pruning.
func (c *cacheFiles) publish(staged, name string) error {
	return os.Rename(staged, filepath.Join(c.dir, name))
}

// remove keeps inventory accurate even if another process removed the file.
func (c *cacheFiles) remove(name string) bool {
	err := os.Remove(filepath.Join(c.dir, name))
	if err != nil && !os.IsNotExist(err) {
		return false
	}
	delete(c.files, name)
	return true
}

// written accounts for a successful publication without rescanning the directory.
func (c *cacheFiles) written(name string, size int) {
	if c.files != nil {
		c.files[name] = cacheEntry{int64(size), time.Now()}
	}
}

// prune evicts oldest-written entries and returns their names for owner metadata
// cleanup. The current publication is retained. Hits never rewrite timestamps.
func (c *cacheFiles) prune(current string) []string {
	if c.files == nil {
		entries, err := os.ReadDir(c.dir)
		if err != nil {
			return nil
		}
		c.files = make(map[string]cacheEntry)
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), c.suffix) {
				continue
			}
			info, err := entry.Info()
			if err == nil && info.Mode().IsRegular() {
				c.files[entry.Name()] = cacheEntry{info.Size(), info.ModTime()}
			}
		}
	}
	var total int64
	for _, entry := range c.files {
		total += entry.size
	}
	if len(c.files) <= c.maxEntries && total <= c.maxBytes {
		return nil
	}
	names := make([]string, 0, len(c.files))
	for name := range c.files {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := c.files[names[i]].written, c.files[names[j]].written
		if a.Equal(b) {
			return names[i] < names[j]
		}
		return a.Before(b)
	})
	var removed []string
	for _, name := range names {
		if len(c.files) <= c.maxEntries && total <= c.maxBytes {
			break
		}
		size := c.files[name].size
		if name != current && c.remove(name) {
			total -= size
			removed = append(removed, name)
		}
	}
	return removed
}
