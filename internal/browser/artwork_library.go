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
func (l *artworkLoader) loadLibrary(ctx context.Context, item jellyfin.Item, emit func(artUpdate)) {
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
func (l *artworkLoader) loadCount(ctx context.Context, item jellyfin.Item, emit func(artUpdate)) {
	lib := l.cache.library(item.ID)
	if time.Now().Before(lib.countUntil) {
		emit(artUpdate{kind: "count", count: lib.count})
		return
	}
	count, err := l.client.LibraryCount(ctx, item)
	if ctx.Err() != nil {
		return
	}
	if err == nil {
		l.cache.rememberLibrary(item.ID, func(value *cachedLibrary) { value.count = count; value.countUntil = time.Now().Add(libraryCacheTTL) })
	}
	emit(artUpdate{kind: "count", count: count, err: err})
}

// loadCovers waits for selection to settle, refreshes the sample if needed, and
// distributes its slots across at most three workers. Each completed image can
// render immediately without waiting for the other slots.
func (l *artworkLoader) loadCovers(ctx context.Context, item jellyfin.Item, emit func(artUpdate)) {
	if !artworkDelay(ctx) {
		return
	}
	lib := l.cache.library(item.ID)
	items := lib.items
	if !time.Now().Before(lib.itemsUntil) {
		page, err := l.client.Mosaic(ctx, item)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			emit(artUpdate{kind: "covers", err: err})
			return
		}
		items = page.Items
		l.cache.rememberLibrary(item.ID, func(value *cachedLibrary) { value.items = items; value.itemsUntil = time.Now().Add(libraryCacheTTL) })
	}
	// Workers take the next cover as soon as one completes. No goroutine per
	// library item is needed, and completed covers retain their sample order.
	var wg sync.WaitGroup
	defer wg.Wait()
	jobs := make(chan int, len(items))
	for i := range items {
		jobs <- i
	}
	close(jobs)
	for worker := 0; worker < min(3, len(items)); worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				im, err := l.fetchImage(ctx, items[i], "Primary")
				if ctx.Err() == nil {
					emit(artUpdate{kind: "cover", image: im, slot: i, total: len(items), err: err})
				}
			}
		}()
	}
}
