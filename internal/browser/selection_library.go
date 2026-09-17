package browser

import (
	"context"
	"image"
	"sync"
	"time"

	"mistervision/internal/artwork"
	"mistervision/internal/media"
)

// loadLibrary lets counts arrive while cover sampling and images are pending.
// Live TV has neither counts nor carousel covers. Both branches finish before
// this method returns, including when the selection context is canceled.
func (l *selectionLoader) loadLibrary(ctx context.Context, item media.Item, emit func(selectionUpdate)) {
	if item.ID == continueID {
		l.loadHomeArtwork(ctx, emit)
		return
	}
	if item.CollectionType == "livetv" {
		return
	}
	if l.customBackground {
		l.loadCount(ctx, item, emit)
		return
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); l.loadCount(ctx, item, emit) }()
	defer wg.Wait()
	l.loadCovers(ctx, item, emit)
}

// loadCount reuses an unexpired total or refreshes it without an image debounce.
func (l *selectionLoader) loadCount(ctx context.Context, item media.Item, emit func(selectionUpdate)) {
	lib := l.libraries.cached(item.ID)
	if time.Now().Before(lib.countUntil) {
		emit(selectionUpdate{kind: selectionCount, count: lib.count})
		return
	}
	count, err := l.client.LibraryCount(ctx, item)
	if ctx.Err() != nil {
		return
	}
	if err == nil {
		l.libraries.remember(item.ID, func(value *cachedLibrary) { value.count = count; value.countUntil = time.Now().Add(libraryCacheTTL) })
	}
	emit(selectionUpdate{kind: selectionCount, count: count, err: err})
}

// loadCovers waits for selection to settle and refreshes the sample if needed.
// It delegates the resolved sample to artwork.Loader for progressive image loads.
func (l *selectionLoader) loadCovers(ctx context.Context, item media.Item, emit func(selectionUpdate)) {
	if !selectionDelay(ctx) {
		return
	}
	lib := l.libraries.cached(item.ID)
	// Disk reads run in this worker. The browser loop never waits for the SD
	// card, and saved images can appear before metadata revalidation finishes.
	if lib.discardMosaic {
		l.disk.Forget(item.ID, item.CollectionType)
		l.libraries.remember(item.ID, func(value *cachedLibrary) { value.discardMosaic = false })
	}
	if l.missingCovers(lib) {
		l.restoreMosaic(ctx, item)
		lib = l.libraries.cached(item.ID)
	}
	items := lib.items
	if len(items) > 0 {
		l.emitCovers(items, emit)
	}
	if !time.Now().Before(lib.itemsUntil) {
		page, err := l.client.Mosaic(ctx, item)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			emit(selectionUpdate{kind: selectionArtwork, err: err})
			return
		}
		items = page.Items
		l.libraries.remember(item.ID, func(value *cachedLibrary) { value.items = items; value.itemsUntil = time.Now().Add(libraryCacheTTL) })
	}
	if ctx.Err() != nil {
		return
	}
	// Replace the sample as a unit, including an empty library. Individual
	// downloads then fill only the slots whose tagged images were not cached.
	l.emitCovers(items, emit)
	covers := make([]artwork.Cover, len(items))
	for i, item := range items {
		covers[i] = artwork.Cover{ID: item.ID, Tag: item.ImageTags["Primary"]}
	}
	l.coverImages(ctx, items, func(update artUpdate) {
		covers[update.slot].Image = update.image
		emit(selectionUpdate{kind: selectionArtwork, art: update, err: update.err})
	})
	l.disk.Save(ctx, item.ID, item.CollectionType, covers)
}

func (l *selectionLoader) missingCovers(lib cachedLibrary) bool {
	if lib.items == nil && lib.itemsUntil.IsZero() {
		return true
	}
	for _, item := range lib.items {
		if item.ImageTags["Primary"] != "" && l.artwork.Cached(item, "Primary") == nil {
			return true
		}
	}
	return false
}

// restoreMosaic primes decoded images and cold sample metadata without marking
// that metadata fresh. server still checks IDs and image tags on each launch.
func (l *selectionLoader) restoreMosaic(ctx context.Context, item media.Item) {
	covers, ok := l.disk.Load(item.ID, item.CollectionType)
	if !ok || ctx.Err() != nil {
		return
	}
	l.artwork.Restore(ctx, covers)
	items := make([]media.Item, len(covers))
	for i, cover := range covers {
		items[i] = media.Item{ID: cover.ID, ImageTags: map[string]string{"Primary": cover.Tag}}
	}
	l.libraries.remember(item.ID, func(value *cachedLibrary) {
		if value.items == nil && value.itemsUntil.IsZero() {
			value.items = items
		}
	})
}

func (l *selectionLoader) emitCovers(items []media.Item, emit func(selectionUpdate)) {
	covers := make([]image.Image, len(items))
	for i, item := range items {
		covers[i] = l.artwork.Cached(item, "Primary")
	}
	emit(selectionUpdate{kind: selectionArtwork, art: artUpdate{kind: "covers", covers: covers}})
}
