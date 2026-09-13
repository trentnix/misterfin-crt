package browser

import (
	"context"
	"errors"
	"image"
	"image/draw"
	"sync"

	"misterfin-crt/internal/jellyfin"
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
	disk := l.disk
	if kind == "Photo" {
		disk = nil
	}
	revision := disk.revision(key)
	if im := disk.load(key, revision); im != nil && ctx.Err() == nil {
		if l.remember(ctx, key, disk, revision, im) {
			return im, nil
		}
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
		if l.remember(ctx, key, disk, revision, im) {
			disk.save(ctx, key, revision, im)
		}
	}
	return im, err
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
