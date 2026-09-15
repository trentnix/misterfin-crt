package browser

import (
	"context"
	"image"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"misterfin-crt/internal/jellyfin"
)

func TestArtworkLoaderOnlyFetchesImagesAndSharesRequestLimit(t *testing.T) {
	png := artPNG(t)
	var active, peak, calls atomic.Int32
	started := make(chan struct{}, 6)
	release := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/Images/") {
			t.Errorf("image loader requested metadata: %s", r.URL.Path)
			http.Error(w, "unexpected metadata request", 500)
			return
		}
		calls.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		started <- struct{}{}
		<-release
		w.Write(png)
	}))
	defer server.Close()
	defer once.Do(func() { close(release) })
	loader := newSelectionLoader(jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{}), 640, 240, selectionCaches{})
	var wg sync.WaitGroup
	var completed atomic.Int32
	for _, id := range []string{"a", "b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item := jellyfin.Item{ID: id, ImageTags: map[string]string{"Primary": "p", "Logo": "l"}, BackdropImageTags: []string{"b"}}
			loader.itemImages(context.Background(), item, true, func(update artUpdate) {
				if update.err != nil {
					t.Error(update.err)
					return
				}
				if _, ok := update.image.(*image.RGBA); !ok {
					t.Errorf("image not normalized to RGBA: %T", update.image)
				}
				completed.Add(1)
			})
		}()
	}
	for range 3 {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("three image requests did not start")
		}
	}
	select {
	case <-started:
		t.Error("overlapping loads exceeded the shared three-request limit")
	case <-time.After(40 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	wg.Wait()
	if calls.Load() != 6 || completed.Load() != 6 || peak.Load() > 3 {
		t.Fatalf("calls=%d completed=%d concurrency=%d", calls.Load(), completed.Load(), peak.Load())
	}
}
