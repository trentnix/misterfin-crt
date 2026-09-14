package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"misterfin-crt/internal/jellyfin"
)

func TestMosaicRestartRestoresBeforeRefreshAndReusesTaggedImages(t *testing.T) {
	png := artPNG(t)
	var imageRequests atomic.Int32
	var blocked, changed, empty, failed atomic.Bool
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Items" {
			if blocked.Load() {
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
			}
			if failed.Load() {
				http.Error(w, "unavailable", 503)
				return
			}
			tag := "first"
			if changed.Load() {
				tag = "changed"
			}
			items := []jellyfin.Item{
				{ID: "a", ImageTags: map[string]string{"Primary": tag}},
				{ID: "b", ImageTags: map[string]string{"Primary": "second"}},
			}
			if empty.Load() {
				items = nil
			}
			json.NewEncoder(w).Encode(jellyfin.Page{Items: items})
			return
		}
		imageRequests.Add(1)
		w.Write(png)
	}))
	defer server.Close()
	root := t.TempDir()
	makeLoader := func() *selectionLoader {
		client := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{UserID: "user"})
		return newSelectionLoader(client, 640, 240, selectionCaches{
			mosaics: newMosaicDiskCache(root, server.URL, "user"),
		})
	}
	library := jellyfin.Item{ID: "library", CollectionType: "movies"}
	first := makeLoader()
	first.loadCovers(context.Background(), library, func(selectionUpdate) {})
	if imageRequests.Load() != 2 {
		t.Fatal("cold load did not fetch both images")
	}

	// A new loader has no memory cache. Hold the metadata request open and
	// require the saved collage to arrive before the server can answer it.
	blocked.Store(true)
	restarted := makeLoader()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan selectionUpdate, 16)
	done := make(chan struct{})
	go func() { restarted.loadCovers(ctx, library, func(u selectionUpdate) { updates <- u }); close(done) }()
	u := receiveSelection(t, updates, "covers")
	if len(u.art.covers) != 2 || u.art.covers[0] == nil || u.art.covers[1] == nil {
		t.Fatal("restart waited for the server instead of restoring the saved collage")
	}
	if imageRequests.Load() != 2 {
		t.Fatal("restoration downloaded artwork")
	}
	close(release)
	awaitSelection(t, done)
	if imageRequests.Load() != 2 {
		t.Fatal("unchanged tags caused image downloads after restart")
	}
	blocked.Store(false)

	// Changed tags must invalidate one image even if the item count is unchanged.
	changed.Store(true)
	restarted.libraries.remember(library.ID, func(v *cachedLibrary) { v.itemsUntil = time.Time{} })
	restarted.loadCovers(context.Background(), library, func(selectionUpdate) {})
	if imageRequests.Load() != 3 {
		t.Fatal("tag refresh did not fetch exactly the changed image")
	}

	// A failed refresh retains the saved collage. It must not overwrite it.
	failed.Store(true)
	fallback := makeLoader()
	var retained bool
	fallback.loadCovers(context.Background(), library, func(u selectionUpdate) {
		if u.art.kind == "covers" && len(u.art.covers) == 2 && u.art.covers[0] != nil {
			retained = true
		}
	})
	if !retained || imageRequests.Load() != 3 {
		t.Fatal("refresh failure lost cached artwork")
	}
	failed.Store(false)

	// A successful empty sample clears and replaces the old collage.
	empty.Store(true)
	restarted.libraries.remember(library.ID, func(v *cachedLibrary) { v.itemsUntil = time.Time{} })
	var cleared bool
	restarted.loadCovers(context.Background(), library, func(u selectionUpdate) {
		if u.art.kind == "covers" && len(u.art.covers) == 0 {
			cleared = true
		}
	})
	covers, ok := makeLoader().disk.load(library.ID, library.CollectionType)
	if !cleared || !ok || len(covers) != 0 {
		t.Fatal("empty library retained an obsolete collage")
	}
}
