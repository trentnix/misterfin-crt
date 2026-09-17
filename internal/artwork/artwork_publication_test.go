package artwork

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mistervision/internal/jellyfin"
)

func TestRetryRejectsOlderInFlightImage(t *testing.T) {
	old := image.NewRGBA(image.Rect(0, 0, 2, 2))
	fresh := image.NewRGBA(image.Rect(0, 0, 2, 2))
	old.Set(0, 0, color.RGBA{R: 255, A: 255})
	fresh.Set(0, 0, color.RGBA{G: 255, A: 255})
	encode := func(im image.Image) []byte {
		var b bytes.Buffer
		if err := png.Encode(&b, im); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	oldPNG, freshPNG := encode(old), encode(fresh)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			_, _ = w.Write(oldPNG)
		} else {
			_, _ = w.Write(freshPNG)
		}
	}))
	defer server.Close()
	defer once.Do(func() { close(release) })
	disk := NewDiskCache(t.TempDir(), server.URL, "user")
	l := NewLoader(jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{}), 640, 240, disk)
	item := jellyfin.Item{ID: "item", ImageTags: map[string]string{"Primary": "tag"}}
	done := make(chan struct{})
	go func() { defer close(done); _, _ = l.Fetch(context.Background(), item, "Primary") }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	l.Forget(item)
	im, err := l.Fetch(context.Background(), item, "Primary")
	if err != nil || im == nil {
		t.Fatal("fresh request failed", err)
	}
	once.Do(func() { close(release) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("old request did not finish")
	}
	if l.Cached(item, "Primary").At(0, 0) != fresh.At(0, 0) {
		t.Fatal("old worker replaced fresh memory")
	}
	key := artworkKey(item, "Primary")
	persisted := disk.load(key, disk.revision(key))
	if persisted == nil || persisted.At(0, 0) != fresh.At(0, 0) {
		t.Fatal("old worker replaced fresh disk image")
	}
	// A saved collage also must not restore the explicitly invalidated image.
	l.Forget(item)
	l.Restore(context.Background(), []Cover{{ID: item.ID, Tag: "tag", Image: old}})
	if l.Cached(item, "Primary") != nil {
		t.Fatal("mosaic bypassed retry invalidation")
	}
}

func TestConcurrentArtworkPublicationIsComplete(t *testing.T) {
	c := NewDiskCache(t.TempDir(), "server", "user")
	key := imageKey{"item", "Primary", "tag"}
	im := image.NewRGBA(image.Rect(0, 0, 20, 20))
	c.save(context.Background(), key, c.revision(key), im)
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				c.save(context.Background(), key, c.revision(key), im)
			}
		}()
	}
	for range 60 {
		data, err := os.ReadFile(filepath.Join(c.dir, artworkFileName(key)))
		if err != nil || decodeCachedArtwork(data) == nil {
			t.Error("reader saw incomplete publication", err)
			break
		}
	}
	wg.Wait()
	entries, err := os.ReadDir(c.dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("temporary files leaked", err)
	}
}
