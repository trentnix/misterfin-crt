package browser

import (
	"context"

	"misterfin-go/internal/jellyfin"
)

// artworkLoader orchestrates progressive requests for one authenticated session.
// It owns a cache and a three-request image limit. Set photo dimensions before
// starting workers. The client must remain authenticated and unchanged while used.
type artworkLoader struct {
	client                  *jellyfin.Client
	photoWidth, photoHeight int
	slots                   chan struct{}
	cache                   artworkCache
}

func newArtworkLoader(client *jellyfin.Client) *artworkLoader {
	return &artworkLoader{
		client: client, photoWidth: 640, photoHeight: 288,
		slots: make(chan struct{}, 3), cache: newArtworkCache(),
	}
}

// load delivers results independently and waits for its workers before returning.
// emit may run concurrently and must return promptly. The caller rejects stale
// selections. Metadata, counts, photos, and detail artwork start immediately.
// List and carousel images wait for selection debounce. At most three image
// requests run across all loads sharing this loader.
func (l *artworkLoader) load(ctx context.Context, item jellyfin.Item, root, detail bool, emit func(artUpdate)) {
	if detail && item.Type == "Photo" {
		im, err := l.fetchImage(ctx, item, "Photo")
		if ctx.Err() == nil {
			emit(artUpdate{kind: "Photo", image: im, err: err})
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
		emit(artUpdate{kind: "detail", detail: &updated, err: err})
		if err == nil {
			item = updated
		}
	} else if !artworkDelay(ctx) {
		return
	}
	l.itemImages(ctx, item, detail, emit)
}
