package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"misterfin-go/internal/jellyfin"
)

func artPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func receiveArt(t *testing.T, updates <-chan artUpdate, kind string) artUpdate {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case update := <-updates:
			if update.kind == kind {
				return update
			}
		case <-timer.C:
			t.Fatalf("missing %s update", kind)
			return artUpdate{}
		}
	}
}
func awaitArt(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("artwork load did not finish")
	}
}
func TestDetailsAndCoverArriveBeforeSlowArtwork(t *testing.T) {
	png := artPNG(t)
	primary := make(chan struct{})
	rest := make(chan struct{})
	var primaryOnce, restOnce sync.Once
	release := func() { primaryOnce.Do(func() { close(primary) }); restOnce.Do(func() { close(rest) }) }
	started := make(chan string, 3)
	var calls atomic.Int32
	var watched atomic.Bool
	item := jellyfin.Item{ID: "movie", Type: "Movie", ImageTags: map[string]string{"Primary": "p", "Logo": "l"}, BackdropImageTags: []string{"b"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Items/movie" {
			value := item
			value.Overview = "Metadata is ready"
			value.UserData.Played = watched.Load()
			json.NewEncoder(w).Encode(value)
			return
		}
		calls.Add(1)
		kind := strings.Split(r.URL.Path, "/")[4]
		started <- kind
		wait := rest
		if kind == "Primary" {
			wait = primary
		}
		select {
		case <-wait:
			w.Write(png)
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer release()
	loader := newArtworkLoader(jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{}))
	updates := make(chan artUpdate, 16)
	done := make(chan struct{})
	go func() {
		loader.load(context.Background(), item, false, true, func(u artUpdate) { updates <- u })
		close(done)
	}()
	update := receiveArt(t, updates, "detail")
	if update.err != nil || update.detail.Overview != "Metadata is ready" {
		t.Fatalf("metadata: %+v", update)
	}
	for i := 0; i < 3; i++ {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("images did not start concurrently")
		}
	}
	primaryOnce.Do(func() { close(primary) })
	update = receiveArt(t, updates, "Primary")
	if update.err != nil || update.image == nil {
		t.Fatal("cover waited for other images")
	}
	select {
	case <-done:
		t.Fatal("slow images should still be pending")
	default:
	}
	release()
	awaitArt(t, done)
	// A details refresh after playback must refresh watched state without fetching
	// unchanged images again. A list view can reuse the very same image entries.
	watched.Store(true)
	loader.load(context.Background(), item, false, true, func(u artUpdate) {
		if u.kind == "detail" && !u.detail.UserData.Played {
			t.Error("stale watched state")
		}
	})
	loader.load(context.Background(), item, false, false, func(artUpdate) {})
	if calls.Load() != 3 {
		t.Fatalf("refetched cached artwork: %d calls", calls.Load())
	}
	snapshot := loader.snapshot(item, false)
	if snapshot.Primary == nil || snapshot.Backdrop == nil || snapshot.Logo == nil {
		t.Fatal("cached artwork not available immediately")
	}
}

func TestCarouselCountsAndCoversLoadIndependently(t *testing.T) {
	png := artPNG(t)
	release := make(chan struct{})
	var once sync.Once
	var imageCalls, active, peak, countCalls, sampleCalls atomic.Int32
	started := make(chan struct{}, 12)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Items" {
			if r.URL.Query().Get("Limit") == "0" {
				countCalls.Add(1)
				fmt.Fprint(w, `{"Items":[],"TotalRecordCount":503}`)
				return
			}
			sampleCalls.Add(1)
			items := make([]jellyfin.Item, 12)
			for i := range items {
				items[i] = jellyfin.Item{ID: fmt.Sprint(i), ImageTags: map[string]string{"Primary": "tag"}}
			}
			json.NewEncoder(w).Encode(jellyfin.Page{Items: items})
			return
		}
		imageCalls.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		started <- struct{}{}
		select {
		case <-release:
			w.Write(png)
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer once.Do(func() { close(release) })
	loader := newArtworkLoader(jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{}))
	item := jellyfin.Item{ID: "movies", CollectionType: "movies"}
	updates := make(chan artUpdate, 32)
	done := make(chan struct{})
	go func() {
		loader.load(context.Background(), item, true, false, func(u artUpdate) { updates <- u })
		close(done)
	}()
	update := receiveArt(t, updates, "count")
	if update.err != nil || update.count == nil || *update.count != 503 {
		t.Fatalf("count: %+v", update)
	}
	for i := 0; i < 3; i++ {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("three image requests did not start")
		}
	}
	if peak.Load() > 3 {
		t.Fatal("unbounded image requests")
	}
	once.Do(func() { close(release) })
	awaitArt(t, done)
	covers := 0
	for len(updates) > 0 {
		update = <-updates
		if update.kind == "cover" && update.image != nil {
			covers++
		}
	}
	if covers != 12 || peak.Load() != 3 {
		t.Fatalf("covers=%d concurrency=%d", covers, peak.Load())
	}
	snapshot := loader.snapshot(item, true)
	if snapshot.Count == nil || *snapshot.Count != 503 || len(snapshot.Covers) != 12 {
		t.Fatal("carousel cache not immediately reusable")
	}
	loader.load(context.Background(), item, true, false, func(artUpdate) {})
	if countCalls.Load() != 1 || sampleCalls.Load() != 1 || imageCalls.Load() != 12 {
		t.Fatal("warm carousel issued HTTP requests")
	}
	loader.rememberLibrary(item.ID, func(v *cachedLibrary) { v.countUntil = time.Time{}; v.itemsUntil = time.Time{} })
	loader.load(context.Background(), item, true, false, func(artUpdate) {})
	if countCalls.Load() != 2 || sampleCalls.Load() != 2 || imageCalls.Load() != 12 {
		t.Fatal("expired counts/sample did not refresh independently of images")
	}
}

func TestCancelRetainsCompletedCover(t *testing.T) {
	png := artPNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/Primary") {
			w.Write(png)
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	loader := newArtworkLoader(jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{}))
	item := jellyfin.Item{ID: "movie", ImageTags: map[string]string{"Primary": "p"}, BackdropImageTags: []string{"b"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan artUpdate, 4)
	done := make(chan struct{})
	go func() { loader.load(ctx, item, false, false, func(u artUpdate) { updates <- u }); close(done) }()
	receiveArt(t, updates, "Primary")
	cancel()
	awaitArt(t, done)
	if loader.snapshot(item, false).Primary == nil {
		t.Fatal("cancel discarded completed artwork")
	}
}

func TestImageCacheEvictsLeastRecentlyUsedWithinBudget(t *testing.T) {
	loader := newArtworkLoader(nil)
	im := image.NewRGBA(image.Rect(0, 0, 1024, 1024))
	for _, id := range []string{"a", "b", "c", "d"} {
		loader.remember(imageKey{id, "Primary", "tag"}, im)
	}
	loader.cached(imageKey{"a", "Primary", "tag"})
	loader.remember(imageKey{"e", "Primary", "tag"}, im)
	if loader.bytes > artworkBudget || len(loader.images) != 4 {
		t.Fatal("cache exceeded budget")
	}
	if loader.cached(imageKey{"b", "Primary", "tag"}) != nil || loader.cached(imageKey{"a", "Primary", "tag"}) == nil {
		t.Fatal("cache evicted a recently used image")
	}
	loader.forget(jellyfin.Item{ID: "a"})
	if loader.cached(imageKey{"a", "Primary", "tag"}) != nil || loader.cached(imageKey{"c", "Primary", "tag"}) == nil {
		t.Fatal("retry evicted unrelated images")
	}
}
