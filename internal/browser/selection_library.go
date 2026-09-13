package browser

import (
	"context"
	"sync"
	"time"

	"misterfin-go/internal/jellyfin"
)

// loadLibrary lets counts arrive while cover sampling and images are pending.
// Live TV has neither counts nor carousel covers. Both branches finish before
// this method returns, including when the selection context is canceled.
func (l *selectionLoader) loadLibrary(ctx context.Context, item jellyfin.Item, emit func(selectionUpdate)) {
	if item.CollectionType == "livetv" {
		return
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); l.loadCount(ctx, item, emit) }()
	defer wg.Wait()
	l.loadCovers(ctx, item, emit)
}

// loadCount reuses an unexpired total or refreshes it without an image debounce.
func (l *selectionLoader) loadCount(ctx context.Context, item jellyfin.Item, emit func(selectionUpdate)) {
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
// It delegates the resolved sample to artworkLoader for progressive image loads.
func (l *selectionLoader) loadCovers(ctx context.Context, item jellyfin.Item, emit func(selectionUpdate)) {
	if !selectionDelay(ctx) {
		return
	}
	lib := l.libraries.cached(item.ID)
	items := lib.items
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
	l.artwork.coverImages(ctx, items, func(update artUpdate) {
		emit(selectionUpdate{kind: selectionArtwork, art: update, err: update.err})
	})
}
