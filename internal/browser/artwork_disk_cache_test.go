package browser

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"misterfin-go/internal/jellyfin"
)

func TestArtworkPersistsAcrossLoadersAndRefreshesChangedTags(t *testing.T) {
	png := artPNG(t)
	var calls atomic.Int32
	var offline atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if offline.Load() {
			http.Error(w, "offline", 503)
			return
		}
		w.Write(png)
	}))
	defer server.Close()
	root := t.TempDir()
	client := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{UserID: "user"})
	loader := func() *artworkLoader {
		l := newArtworkLoader(client, 640, 240)
		l.disk = newArtworkDiskCache(root, server.URL, "user")
		return l
	}
	item := jellyfin.Item{ID: "series", ImageTags: map[string]string{"Primary": "p", "Logo": "l"}, BackdropImageTags: []string{"b"}}
	ctx := context.Background()
	fetch := func(l *artworkLoader, item jellyfin.Item, kind string) image.Image {
		t.Helper()
		im, err := l.fetchImage(ctx, item, kind)
		if err != nil || im == nil {
			t.Fatalf("%s: %v, %v", kind, im, err)
		}
		return im
	}
	first := loader()
	for _, kind := range []string{"Primary", "Backdrop", "Logo"} {
		fetch(first, item, kind)
	}
	if calls.Load() != 3 {
		t.Fatal("initial downloads", calls.Load())
	}
	key := artworkKey(item, "Primary")
	path := filepath.Join(first.disk.dir, artworkFileName(key))
	before, _ := os.Stat(path)
	offline.Store(true)
	second := loader()
	for _, kind := range []string{"Primary", "Backdrop", "Logo"} {
		fetch(second, item, kind)
	}
	episode := jellyfin.Item{ID: "episode", ParentBackdropItemId: "series", ParentBackdropImageTags: []string{"b"}}
	fetch(loader(), episode, "Backdrop")
	after, _ := os.Stat(path)
	if calls.Load() != 3 || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("disk hits downloaded or rewrote artwork")
	}
	offline.Store(false)
	item.ImageTags["Primary"] = "new-tag"
	fetch(second, item, "Primary")
	if calls.Load() != 4 {
		t.Fatal("changed tag did not fetch new artwork")
	}
	second.forget(item)
	fetch(second, item, "Primary")
	if calls.Load() != 5 {
		t.Fatal("explicit retry reused old pixels")
	}
	// Corruption must repair itself with a successful network fetch.
	path = filepath.Join(second.disk.dir, artworkFileName(artworkKey(item, "Primary")))
	if err := os.WriteFile(path, []byte("truncated"), 0600); err != nil {
		t.Fatal(err)
	}
	fetch(loader(), item, "Primary")
	if calls.Load() != 6 {
		t.Fatal("corrupt entry did not fall back to Jellyfin")
	}
	offline.Store(true)
	fetch(loader(), item, "Primary")
	if calls.Load() != 6 {
		t.Fatal("repaired entry did not persist")
	}
}

func TestArtworkDiskFormatPreservesAlphaAndRejectsCorruption(t *testing.T) {
	im := image.NewRGBA(image.Rect(10, 20, 14, 23))
	im.Set(11, 21, color.NRGBA{200, 100, 40, 128})
	data := encodeArtwork(im.SubImage(image.Rect(11, 21, 13, 23)))
	got := decodeCachedArtwork(data)
	if got == nil || got.Bounds().Dx() != 2 || got.At(0, 0) != im.At(11, 21) {
		t.Fatal("subimage or alpha changed")
	}
	if got.At(1, 1) != (color.RGBA{}) {
		t.Fatal("transparent pixel became opaque")
	}
	for _, broken := range [][]byte{nil, data[:len(data)-1], append(append([]byte{}, data...), 0), bytes.Repeat([]byte{255}, 24)} {
		if decodeCachedArtwork(broken) != nil {
			t.Fatal("accepted invalid cache")
		}
	}
	data[16] ^= 1
	if decodeCachedArtwork(data) != nil {
		t.Fatal("checksum failed to detect corruption")
	}
	if encodeArtwork(image.NewRGBA(image.Rect(0, 0, 641, 360))) != nil {
		t.Fatal("accepted oversized image")
	}
}

func TestArtworkDiskIsolationCancellationAndRetry(t *testing.T) {
	root := t.TempDir()
	c := newArtworkDiskCache(root, "http://server/", "user")
	key := imageKey{"id", "Logo", "tag"}
	im := image.NewRGBA(image.Rect(0, 0, 2, 2))
	ctx := context.Background()
	rev := c.revision(key)
	c.save(ctx, key, rev, im)
	for _, other := range []*artworkDiskCache{newArtworkDiskCache(root, "http://other", "user"), newArtworkDiskCache(root, "http://server", "other")} {
		if other.load(key, other.revision(key)) != nil {
			t.Fatal("cross-account artwork leak")
		}
	}
	if newArtworkDiskCache(root, "http://server", "user").load(key, rev) == nil {
		t.Fatal("trailing slash changed namespace")
	}
	for _, other := range []imageKey{{"other", "Logo", "tag"}, {"id", "Primary", "tag"}, {"id", "Logo", "other"}} {
		if c.load(other, c.revision(other)) != nil {
			t.Fatal("image identities collided")
		}
	}
	c.invalidate(key)
	fresh := c.revision(key)
	if c.load(key, fresh) != nil {
		t.Fatal("retry loaded stale image")
	}
	c.save(ctx, key, rev, im)
	if newArtworkDiskCache(root, "http://server", "user").load(key, rev) != nil {
		t.Fatal("stale worker recreated invalidated file")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	c.save(canceled, key, fresh, im)
	if c.load(key, fresh) != nil {
		t.Fatal("canceled request persisted")
	}
	c.save(ctx, key, fresh, im)
	if c.load(key, c.revision(key)) == nil || c.load(key, rev) != nil {
		t.Fatal("retry revision was not enforced")
	}
	l := newArtworkLoader(nil, 640, 240)
	l.disk = c
	rev = c.revision(key)
	l.forget(jellyfin.Item{ID: key.id, ImageTags: map[string]string{"Logo": key.tag}})
	if l.remember(ctx, key, c, rev, im) || l.cache.cached(key) != nil {
		t.Fatal("stale worker repopulated memory after retry")
	}
}

func TestArtworkDiskFailureDoesNotBlockImagesAndPhotosStayInMemory(t *testing.T) {
	png := artPNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(png) }))
	defer server.Close()
	root := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(root, nil, 0600); err != nil {
		t.Fatal(err)
	}
	l := newArtworkLoader(jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{}), 640, 240)
	l.disk = newArtworkDiskCache(root, server.URL, "user")
	item := jellyfin.Item{ID: "id", ImageTags: map[string]string{"Primary": "tag"}}
	if im, err := l.fetchImage(context.Background(), item, "Primary"); im == nil || err != nil {
		t.Fatal("cache failure broke loading", err)
	}
	l.disk = newArtworkDiskCache(t.TempDir(), server.URL, "user")
	if im, err := l.fetchImage(context.Background(), item, "Photo"); im == nil || err != nil {
		t.Fatal("photo failed", err)
	}
	if _, err := os.Stat(l.disk.dir); !os.IsNotExist(err) {
		t.Fatal("photo created persistent cache")
	}
}

func TestArtworkDiskPruning(t *testing.T) {
	for _, size := range []int64{20, 1024 * 1024} {
		c := newArtworkDiskCache(t.TempDir(), "server", "user")
		if err := os.MkdirAll(c.dir, 0700); err != nil {
			t.Fatal(err)
		}
		for i := 0; i <= artworkDiskEntries; i++ {
			f, err := os.Create(filepath.Join(c.dir, fmt.Sprintf("%03d.rgba", i)))
			if err != nil {
				t.Fatal(err)
			}
			err = f.Truncate(size)
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		c.prune("000.rgba")
		files, _ := os.ReadDir(c.dir)
		var total int64
		for _, f := range files {
			info, _ := f.Info()
			total += info.Size()
		}
		if len(files) > artworkDiskEntries || total > artworkDiskBudget {
			t.Fatal("cache exceeded limits", len(files), total)
		}
		if _, err := os.Stat(filepath.Join(c.dir, "000.rgba")); err != nil {
			t.Fatal("pruned current image", err)
		}
	}
}

func TestArtworkCacheLocations(t *testing.T) {
	t.Setenv("MISTERFIN_CACHE_ROOT", "")
	if got := browserCacheRoot(false, "covercache"); got != "/media/fat/misterfin-go/covercache" {
		t.Fatal(got)
	}
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", root)
	if got := browserCacheRoot(true, "covercache"); got != filepath.Join(root, "misterfin-go", "covercache") {
		t.Fatal(got)
	}
	t.Setenv("MISTERFIN_CACHE_ROOT", root)
	if got := browserCacheRoot(false, "covercache"); got != filepath.Join(root, "misterfin-go", "covercache") {
		t.Fatal(got)
	}
}

func BenchmarkArtworkDiskRestore(b *testing.B) {
	c := newArtworkDiskCache(b.TempDir(), "server", "user")
	key := imageKey{"id", "Backdrop", "tag"}
	im := image.NewRGBA(image.Rect(0, 0, 640, 360))
	revision := c.revision(key)
	c.save(context.Background(), key, revision, im)
	b.SetBytes(int64(len(im.Pix)))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if c.load(key, revision) == nil {
			b.Fatal("disk cache miss")
		}
	}
}
