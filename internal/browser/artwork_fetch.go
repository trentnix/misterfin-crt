package browser

import (
	"context"
	"errors"
	"image"
	"image/draw"
	"sync"
	"time"

	"misterfin-go/internal/jellyfin"
)

// fetchImage reuses tagged artwork before acquiring a request slot and checks
// the cache again afterward. Successful uncanceled loads are normalized to RGBA
// and retained. The image must not be mutated after returning.
func (l *artworkLoader) fetchImage(ctx context.Context, item jellyfin.Item, kind string) (image.Image, error) {
	key := artworkKey(item, kind)
	if key.tag == "" {
		if kind == "Photo" {
			return nil, errors.New("photo unavailable")
		}
		return nil, nil
	}
	if im := l.cache.cached(key); im != nil {
		return im, nil
	}
	select {
	case l.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-l.slots }()
	if im := l.cache.cached(key); im != nil {
		return im, nil
	}
	var im image.Image
	var err error
	if kind == "Photo" {
		im, err = l.client.Photo(ctx, item, l.photoWidth, l.photoHeight)
	} else {
		im, err = l.client.ImageKind(ctx, item, kind)
	}
	if err == nil && ctx.Err() == nil {
		if im != nil {
			if _, ok := im.(*image.RGBA); !ok {
				b := im.Bounds()
				rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
				draw.Draw(rgba, rgba.Bounds(), im, b.Min, draw.Src)
				im = rgba
			}
		}
		l.cache.remember(key, im)
	}
	return im, err
}

// artworkDelay debounces list and carousel selections for 120 milliseconds.
// Cancellation returns false without starting an image request.
func artworkDelay(ctx context.Context) bool {
	timer := time.NewTimer(120 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// itemImages requests each image independently and waits for all workers.
// emit may run concurrently and must return promptly.
func (l *artworkLoader) itemImages(ctx context.Context, item jellyfin.Item, detail bool, emit func(artUpdate)) {
	kinds := []string{"Primary", "Backdrop"}
	if detail {
		kinds = append(kinds, "Logo")
	}
	var wg sync.WaitGroup
	for _, kind := range kinds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			im, err := l.fetchImage(ctx, item, kind)
			if ctx.Err() == nil {
				emit(artUpdate{kind: kind, image: im, err: err})
			}
		}()
	}
	wg.Wait()
}
