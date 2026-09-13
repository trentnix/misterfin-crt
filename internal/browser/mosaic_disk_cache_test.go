package browser

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testMosaic() []mosaicCover {
	im := image.NewRGBA(image.Rect(3, 4, 5, 6))
	im.SetRGBA(3, 4, color.RGBA{10, 20, 30, 255})
	return []mosaicCover{{imageKey{"cover", "Primary", "tag"}, im}, {key: imageKey{"empty", "Primary", ""}}}
}

func TestMosaicDiskSurvivesRestartWithoutRewriting(t *testing.T) {
	root := t.TempDir()
	c := newMosaicDiskCache(root, "http://server", "user")
	c.save(context.Background(), "library", "movies", testMosaic())
	path := filepath.Join(c.dir, mosaicFileName("library", "movies"))
	old := time.Unix(100, 0)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	restarted := newMosaicDiskCache(root, "http://server/", "user")
	covers, ok := restarted.load("library", "movies")
	if !ok || len(covers) != 2 || covers[0].image.At(0, 0) != (color.RGBA{10, 20, 30, 255}) || covers[1].image != nil {
		t.Fatal("restart did not restore exact decoded pixels and empty slots")
	}
	restarted.save(context.Background(), "library", "movies", covers)
	info, err := os.Stat(path)
	if err != nil || !info.ModTime().Equal(old) {
		t.Fatal("unchanged mosaic rewrote the cache")
	}
	for _, other := range []*mosaicDiskCache{
		newMosaicDiskCache(root, "http://other", "user"),
		newMosaicDiskCache(root, "http://server", "other"),
	} {
		if _, ok := other.load("library", "movies"); ok {
			t.Fatal("cache leaked across server or user")
		}
	}
	if _, ok := restarted.load("library", "music"); ok {
		t.Fatal("cache ignored collection type")
	}
}

func TestMosaicRejectsCorruptionAndOversizedRecords(t *testing.T) {
	data, err := encodeMosaic(testMosaic())
	if err != nil {
		t.Fatal(err)
	}
	bad := append([]byte(nil), data...)
	bad[len(bad)-5] ^= 1
	for _, value := range [][]byte{nil, data[:len(data)-1], bad, append(data, 0)} {
		if _, err := decodeMosaic(value); err == nil {
			t.Fatal("corrupt file accepted")
		}
	}
	// Valid checksum, invalid dimensions: validation must happen before allocation.
	header := []byte(`[{"ID":"cover","Tag":"tag","Width":2147483647,"Height":2147483647}]`)
	bad = make([]byte, 12)
	copy(bad, mosaicMagic)
	binary.LittleEndian.PutUint32(bad[8:], uint32(len(header)))
	bad = append(bad, header...)
	bad = binary.LittleEndian.AppendUint32(bad, crc32.ChecksumIEEE(bad))
	if _, err := decodeMosaic(bad); err == nil {
		t.Fatal("oversized decoded allocation accepted")
	}
	c := newMosaicDiskCache(t.TempDir(), "server", "user")
	c.save(context.Background(), "library", "movies", testMosaic())
	path := filepath.Join(c.dir, mosaicFileName("library", "movies"))
	if err := os.WriteFile(path, bad, 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.load("library", "movies"); ok {
		t.Fatal("bad cache was not treated as a miss")
	}
}

func TestIncompleteCanceledAndUnavailableCacheKeepExistingData(t *testing.T) {
	c := newMosaicDiskCache(t.TempDir(), "server", "user")
	c.save(context.Background(), "library", "movies", testMosaic())
	path := filepath.Join(c.dir, mosaicFileName("library", "movies"))
	before, _ := os.ReadFile(path)
	c.save(context.Background(), "library", "movies", []mosaicCover{{key: imageKey{"broken", "Primary", "tag"}}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.save(ctx, "library", "movies", nil)
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("partial or canceled load replaced usable cache")
	}
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	unavailable := newMosaicDiskCache(blocked, "server", "user")
	unavailable.save(context.Background(), "library", "movies", testMosaic())
	if _, ok := unavailable.load("library", "movies"); ok {
		t.Fatal("unavailable cache was not a miss")
	}
}

func TestMosaicDiskBudgetAndEntryLimit(t *testing.T) {
	c := newMosaicDiskCache(t.TempDir(), "server", "user")
	for i := 0; i < libraryCacheLimit+2; i++ {
		c.save(context.Background(), fmt.Sprint(i), "movies", testMosaic())
	}
	entries, err := os.ReadDir(c.dir)
	if err != nil || len(entries) != libraryCacheLimit {
		t.Fatal("disk entry limit was not enforced")
	}
	if len(c.known) > libraryCacheLimit {
		t.Fatal("fingerprint retention exceeded entry limit")
	}
	// Sparse files exercise byte accounting without writing a large fixture.
	for _, entry := range entries {
		if err := os.Truncate(filepath.Join(c.dir, entry.Name()), 4*1024*1024); err != nil {
			t.Fatal(err)
		}
	}
	current := entries[len(entries)-1].Name()
	c.prune(current)
	entries, _ = os.ReadDir(c.dir)
	var total int64
	for _, entry := range entries {
		info, _ := entry.Info()
		total += info.Size()
	}
	if total > mosaicDiskBudget {
		t.Fatal("disk byte budget was not enforced")
	}
	if _, err := os.Stat(filepath.Join(c.dir, current)); err != nil {
		t.Fatal("pruning removed current mosaic")
	}
}

func TestMosaicCacheLocations(t *testing.T) {
	t.Setenv("MISTERFIN_CACHE_ROOT", "")
	if got := mosaicCacheRoot(false); got != "/media/fat/misterfin-go/gridcache" {
		t.Fatal(got)
	}
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", root)
	if got := mosaicCacheRoot(true); got != filepath.Join(root, "misterfin-go", "gridcache") {
		t.Fatal(got)
	}
	t.Setenv("MISTERFIN_CACHE_ROOT", root)
	if got := mosaicCacheRoot(false); got != filepath.Join(root, "misterfin-go", "gridcache") {
		t.Fatal(got)
	}
}

func BenchmarkMosaicDiskRestore(b *testing.B) {
	c := newMosaicDiskCache(b.TempDir(), "server", "user")
	covers := make([]mosaicCover, 12)
	for i := range covers {
		covers[i] = mosaicCover{imageKey{fmt.Sprint(i), "Primary", "tag"}, image.NewRGBA(image.Rect(0, 0, 320, 360))}
	}
	c.save(context.Background(), "library", "movies", covers)
	b.ReportAllocs()
	b.SetBytes(12 * 320 * 360 * 4)
	b.ResetTimer()
	for b.Loop() {
		if _, ok := c.load("library", "movies"); !ok {
			b.Fatal("cache miss")
		}
	}
}
