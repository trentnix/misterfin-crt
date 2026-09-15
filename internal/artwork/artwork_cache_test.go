package artwork

import (
	"image"
	"testing"

	"misterfin-crt/internal/jellyfin"
)

func TestImageCacheEvictsLeastRecentlyUsedWithinBudget(t *testing.T) {
	cache := newArtworkCache()
	im := image.NewRGBA(image.Rect(0, 0, 1024, 1024))
	for _, id := range []string{"a", "b", "c", "d"} {
		cache.remember(imageKey{id, "Primary", "tag"}, im)
	}
	cache.cached(imageKey{"a", "Primary", "tag"})
	cache.remember(imageKey{"e", "Primary", "tag"}, im)
	if cache.bytes > artworkBudget || len(cache.images) != 4 {
		t.Fatal("cache exceeded budget")
	}
	if cache.cached(imageKey{"b", "Primary", "tag"}) != nil || cache.cached(imageKey{"a", "Primary", "tag"}) == nil {
		t.Fatal("cache evicted a recently used image")
	}
	cache.forget(jellyfin.Item{ID: "a"})
	if cache.cached(imageKey{"a", "Primary", "tag"}) != nil || cache.cached(imageKey{"c", "Primary", "tag"}) == nil {
		t.Fatal("retry evicted unrelated images")
	}
}

func TestArtworkCacheRetryInvalidatesParentBackdrop(t *testing.T) {
	cache := newArtworkCache()
	im := image.NewRGBA(image.Rect(0, 0, 2, 2))
	item := jellyfin.Item{ID: "episode", ParentBackdropItemID: "series", ParentBackdropImageTags: []string{"tag"}}
	shared := artworkKey(item, "Backdrop")
	unrelated := imageKey{"other", "Primary", "tag"}
	cache.remember(shared, im)
	cache.remember(unrelated, im)
	cache.forget(item)
	if cache.cached(shared) != nil || cache.cached(unrelated) != im {
		t.Fatal("retry did not limit invalidation to the item and its parent backdrop")
	}
	if cache.bytes != 16 {
		t.Fatalf("invalid byte accounting: %d", cache.bytes)
	}
}
