package browser

import (
	"context"
	"fmt"
	"image"
	"testing"
	"time"

	"mistervision/internal/artwork"
	"mistervision/internal/media"
)

func TestLibraryCacheBoundsAndIndependentFields(t *testing.T) {
	cache := newLibraryCache()
	until := time.Now().Add(time.Minute)
	for i := range libraryCacheLimit {
		count := i
		cache.remember(fmt.Sprint(i), func(v *cachedLibrary) { v.count = &count; v.countUntil = until })
	}
	cache.cached("0")
	cache.remember("extra", func(v *cachedLibrary) {})
	if len(cache.libraries) != libraryCacheLimit || cache.cached("1").count != nil || cache.cached("0").count == nil {
		t.Fatal("metadata cache did not evict the least recently used library")
	}
	cache.remember("0", func(v *cachedLibrary) { v.items = []media.Item{{ID: "cover"}}; v.itemsUntil = until })
	value := cache.cached("0")
	if value.count == nil || *value.count != 0 || value.countUntil != until || len(value.items) != 1 {
		t.Fatal("refreshing a sample replaced its count")
	}
	cache.remember("0", func(v *cachedLibrary) { v.countUntil = time.Time{} })
	if cache.cached("0").itemsUntil != until {
		t.Fatal("count expiry changed sample expiry")
	}
}

func TestSelectionSnapshotExpiryAndRetry(t *testing.T) {
	loader := newSelectionLoader(nil, 640, 240, selectionCaches{})
	item := media.Item{ID: "library"}
	cover := media.Item{ID: "cover", ImageTags: map[string]string{"Primary": "tag"}}
	im := image.NewRGBA(image.Rect(0, 0, 2, 2))
	loader.artwork.Restore(context.Background(), []artwork.Cover{{ID: cover.ID, Tag: "tag", Image: im}})
	count := 42
	future := time.Now().Add(time.Minute)
	loader.libraries.remember(item.ID, func(v *cachedLibrary) {
		v.count = &count
		v.items = []media.Item{cover}
		v.itemsUntil = future
	})
	data := loader.snapshot(item, true)
	if data.count != nil || len(data.artwork.Covers) != 1 || data.artwork.Covers[0] != im {
		t.Fatal("expired count hid a valid cover sample")
	}
	// The snapshot owns its cover slice, not the metadata sample or image cache.
	data.artwork.Covers[0] = nil
	if loader.snapshot(item, true).artwork.Covers[0] != im {
		t.Fatal("snapshot mutated cached covers")
	}
	loader.libraries.remember(item.ID, func(v *cachedLibrary) { v.countUntil = future; v.itemsUntil = time.Time{} })
	data = loader.snapshot(item, true)
	if data.count == nil || *data.count != 42 || data.artwork.Covers != nil {
		t.Fatal("sample expiry discarded the count or reused stale sample")
	}
	if loader.artwork.Cached(cover, "Primary") != im {
		t.Fatal("metadata expiry evicted an image")
	}
	item.ImageTags = map[string]string{"Primary": "tag"}
	loader.artwork.Restore(context.Background(), []artwork.Cover{{ID: item.ID, Tag: "tag", Image: im}})
	loader.forget(item)
	if loader.libraries.cached(item.ID).count != nil || loader.artwork.Cached(item, "Primary") != nil {
		t.Fatal("retry did not invalidate both metadata and images")
	}
	if loader.artwork.Cached(cover, "Primary") != im {
		t.Fatal("retry evicted an unrelated image")
	}
}
