package browser

import (
	"context"
	"image"
	"time"

	"misterfin-crt/internal/jellyfin"
)

// selectionLoader coordinates metadata and images for one authenticated session.
// It refreshes non-photo details on every visit, caches library metadata, and
// delegates decoded images to artworkLoader. It owns no browser model state.
type selectionLoader struct {
	client    *jellyfin.Client
	artwork   *artworkLoader
	libraries libraryCache
	disk      *mosaicDiskCache
}

// selectionCaches contains account-scoped persistence dependencies. Nil caches
// preserve the same in-memory behavior for disabled storage and tests.
type selectionCaches struct {
	mosaics *mosaicDiskCache
	artwork *artworkDiskCache
}

func newSelectionCaches(config Config, client *jellyfin.Client) selectionCaches {
	return selectionCaches{
		mosaics: newMosaicDiskCache(config.MosaicCacheDir, client.Config.Server, client.Session.UserID),
		artwork: newArtworkDiskCache(config.ArtworkCacheDir, client.Config.Server, client.Session.UserID),
	}
}

// newSelectionLoader returns a complete loader. Callers cannot attach disk
// caches after workers begin using it.
func newSelectionLoader(client *jellyfin.Client, photoWidth, photoHeight int, caches selectionCaches) *selectionLoader {
	artwork := newArtworkLoader(client, photoWidth, photoHeight)
	artwork.disk = caches.artwork
	return &selectionLoader{
		client:    client,
		artwork:   artwork,
		libraries: newLibraryCache(),
		disk:      caches.mosaics,
	}
}

// load delivers results independently and waits for its workers before returning.
// emit may run concurrently and must return promptly. The caller rejects stale
// selections. Metadata, counts, and photos start immediately. Detail images
// follow fresh metadata. List and carousel images wait for debounce. Three image
// requests run across all loads sharing this loader.
func (l *selectionLoader) load(ctx context.Context, item jellyfin.Item, root, detail bool, emit func(selectionUpdate)) {
	if detail && item.Type == "Photo" {
		im, err := l.artwork.fetchImage(ctx, item, "Photo")
		if ctx.Err() == nil {
			emit(selectionUpdate{kind: selectionArtwork, art: artUpdate{kind: "Photo", image: im, err: err}, err: err})
		}
		return
	}
	if root {
		l.loadLibrary(ctx, item, emit)
		return
	}

	if detail {
		updated, err := l.client.Details(ctx, item.ID)
		if ctx.Err() != nil {
			return
		}
		updated.ContinueAction = item.ContinueAction
		emit(selectionUpdate{kind: selectionDetails, detail: &updated, err: err})
		if err == nil {
			item = updated
		}
	} else if !selectionDelay(ctx) {
		return
	}
	l.artwork.itemImages(ctx, item, detail, func(update artUpdate) {
		emit(selectionUpdate{kind: selectionArtwork, art: update, err: update.err})
	})
}

// selectionDelay debounces list and carousel selections for 120 milliseconds.
// Cancellation returns false without starting an image request.
func selectionDelay(ctx context.Context) bool {
	timer := time.NewTimer(120 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// snapshot assembles immediately available selection data without network I/O.
// It omits expired metadata but keeps reusable images. The caller owns the cover
// slice. Images and count values remain immutable after publication.
func (l *selectionLoader) snapshot(item jellyfin.Item, root bool) selectionData {
	if !root {
		return selectionData{artwork: l.artwork.cache.snapshot(item)}
	}
	lib := l.libraries.cached(item.ID)
	data := selectionData{}
	if time.Now().Before(lib.countUntil) {
		data.count = lib.count
	}
	if time.Now().Before(lib.itemsUntil) {
		data.artwork.Covers = make([]image.Image, len(lib.items))
		for i, item := range lib.items {
			data.artwork.Covers[i] = l.artwork.cache.cached(artworkKey(item, "Primary"))
		}
	}
	return data
}

// forget invalidates both metadata and images for an explicit retry. Shared
// parent backdrops follow the image cache's existing invalidation policy.
func (l *selectionLoader) forget(item jellyfin.Item) {
	l.libraries.forget(item.ID)
	l.libraries.remember(item.ID, func(value *cachedLibrary) { value.discardMosaic = true })
	l.artwork.forget(item)
}
