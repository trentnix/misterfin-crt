package browser

import "context"

// loadHomeArtwork reuses the feed's cover sample. A synthetic home card must
// never be sent to Jellyfin as a real library ID or written to the disk cache.
func (l *selectionLoader) loadHomeArtwork(ctx context.Context, emit func(selectionUpdate)) {
	lib := l.libraries.cached(continueID)
	emit(selectionUpdate{kind: selectionCount, count: lib.count})
	if l.customBackground || !selectionDelay(ctx) {
		return
	}
	l.emitCovers(lib.items, emit)
	l.coverImages(ctx, lib.items, func(update artUpdate) { emit(selectionUpdate{kind: selectionArtwork, art: update, err: update.err}) })
}
