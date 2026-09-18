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

	"mistervision/internal/jellyfin"
	"mistervision/internal/media"
)

func artPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func receiveSelection(t *testing.T, updates <-chan selectionUpdate, kind string) selectionUpdate {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case update := <-updates:
			if (update.kind == selectionDetails && kind == "detail") ||
				(update.kind == selectionCount && kind == "count") ||
				(update.kind == selectionArtwork && update.art.kind == kind) {
				return update
			}
		case <-timer.C:
			t.Fatalf("missing %s update", kind)
			return selectionUpdate{}
		}
	}
}
func awaitSelection(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("selection load did not finish")
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
	item := media.Item{ID: "movie", Type: "Movie", ImageTags: map[string]string{"Primary": "p", "Logo": "l"}, BackdropImageTags: []string{"b"}}
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
	loader := newSelectionLoader(jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{}), 640, 288, selectionCaches{})
	updates := make(chan selectionUpdate, 16)
	done := make(chan struct{})
	go func() {
		loader.load(context.Background(), item, false, true, func(u selectionUpdate) { updates <- u })
		close(done)
	}()
	update := receiveSelection(t, updates, "detail")
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
	update = receiveSelection(t, updates, "Primary")
	if update.err != nil || update.art.image == nil {
		t.Fatal("cover waited for other images")
	}
	select {
	case <-done:
		t.Fatal("slow images should still be pending")
	default:
	}
	release()
	awaitSelection(t, done)
	// A details refresh after playback must refresh watched state without fetching
	// unchanged images again. A list view can reuse the very same image entries.
	watched.Store(true)
	loader.load(context.Background(), item, false, true, func(u selectionUpdate) {
		if u.kind == selectionDetails && !u.detail.UserData.Played {
			t.Error("stale watched state")
		}
	})
	loader.load(context.Background(), item, false, false, func(selectionUpdate) {})
	if calls.Load() != 3 {
		t.Fatalf("refetched cached artwork: %d calls", calls.Load())
	}
	snapshot := loader.snapshot(item, false)
	if snapshot.artwork.Primary == nil || snapshot.artwork.Backdrop == nil || snapshot.artwork.Logo == nil {
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
			items := make([]media.Item, 12)
			for i := range items {
				items[i] = media.Item{ID: fmt.Sprint(i), ImageTags: map[string]string{"Primary": "tag"}}
			}
			json.NewEncoder(w).Encode(media.Page{Items: items})
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
	loader := newSelectionLoader(jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{}), 640, 288, selectionCaches{})
	item := media.Item{ID: "movies", CollectionType: "movies"}
	updates := make(chan selectionUpdate, 32)
	done := make(chan struct{})
	go func() {
		loader.load(context.Background(), item, true, false, func(u selectionUpdate) { updates <- u })
		close(done)
	}()
	update := receiveSelection(t, updates, "count")
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
	awaitSelection(t, done)
	covers := 0
	for len(updates) > 0 {
		update = <-updates
		if update.kind == selectionArtwork && update.art.kind == "cover" && update.art.image != nil {
			covers++
		}
	}
	if covers != 12 || peak.Load() != 3 {
		t.Fatalf("covers=%d concurrency=%d", covers, peak.Load())
	}
	snapshot := loader.snapshot(item, true)
	if snapshot.count == nil || *snapshot.count != 503 || len(snapshot.artwork.Covers) != 12 {
		t.Fatal("carousel cache not immediately reusable")
	}
	loader.load(context.Background(), item, true, false, func(selectionUpdate) {})
	if countCalls.Load() != 1 || sampleCalls.Load() != 1 || imageCalls.Load() != 12 {
		t.Fatal("warm carousel issued HTTP requests")
	}
	loader.libraries.remember(item.ID, func(v *cachedLibrary) { v.countUntil = time.Time{} })
	loader.load(context.Background(), item, true, false, func(selectionUpdate) {})
	if countCalls.Load() != 2 || sampleCalls.Load() != 1 || imageCalls.Load() != 12 {
		t.Fatal("expired count refreshed sample or images")
	}
	loader.libraries.remember(item.ID, func(v *cachedLibrary) { v.itemsUntil = time.Time{} })
	loader.load(context.Background(), item, true, false, func(selectionUpdate) {})
	if countCalls.Load() != 2 || sampleCalls.Load() != 2 || imageCalls.Load() != 12 {
		t.Fatal("expired sample refreshed count or images")
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
	loader := newSelectionLoader(jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{}), 640, 288, selectionCaches{})
	item := media.Item{ID: "movie", ImageTags: map[string]string{"Primary": "p"}, BackdropImageTags: []string{"b"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan selectionUpdate, 4)
	done := make(chan struct{})
	go func() { loader.load(ctx, item, false, false, func(u selectionUpdate) { updates <- u }); close(done) }()
	receiveSelection(t, updates, "Primary")
	cancel()
	awaitSelection(t, done)
	if loader.snapshot(item, false).artwork.Primary == nil {
		t.Fatal("cancel discarded completed artwork")
	}
}

func TestCarouselCoversDoNotWaitForSlowCount(t *testing.T) {
	png := artPNG(t)
	countStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Items" {
			if r.URL.Query().Get("Limit") == "0" {
				close(countStarted)
				<-r.Context().Done()
				return
			}
			fmt.Fprint(w, `{"Items":[{"Id":"cover","ImageTags":{"Primary":"tag"}}]}`)
			return
		}
		w.Write(png)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	loader := newSelectionLoader(jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{}), 640, 240, selectionCaches{})
	updates := make(chan selectionUpdate, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		loader.load(ctx, media.Item{ID: "movies", CollectionType: "movies"}, true, false, func(u selectionUpdate) { updates <- u })
	}()
	select {
	case <-countStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("count request did not start")
	}
	update := receiveSelection(t, updates, "cover")
	if update.err != nil || update.art.image == nil {
		t.Fatal("cover waited for count or failed", update.err)
	}
	select {
	case <-done:
		t.Fatal("selection finished with count still pending")
	default:
	}
	cancel()
	awaitSelection(t, done)
	if loader.snapshot(media.Item{ID: "movies"}, true).artwork.Covers[0] == nil {
		t.Fatal("canceling metadata discarded completed cover")
	}
}

func TestSelectionCancellationBeforeDebounceSkipsImages(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	loader := newSelectionLoader(jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{}), 640, 240, selectionCaches{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	loader.load(ctx, media.Item{ID: "item", ImageTags: map[string]string{"Primary": "tag"}}, false, false, func(selectionUpdate) { t.Error("canceled list selection emitted an update") })
	if calls.Load() != 0 {
		t.Fatal("canceled selection started an image request")
	}
}
