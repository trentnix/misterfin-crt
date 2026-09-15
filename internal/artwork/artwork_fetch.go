package artwork

import (
	"context"
	"errors"
	"image"
	"image/draw"

	"misterfin-crt/internal/jellyfin"
)

// Fetch reuses tagged artwork before acquiring a request slot and checks
// the cache again afterward. Successful uncanceled loads are normalized to RGBA
// and retained. Kind must be Primary, Backdrop, Logo, or Photo. The returned
// image is immutable. Call Fetch on a worker, not the browser event loop.
func (l *Loader) Fetch(ctx context.Context, item jellyfin.Item, kind string) (image.Image, error) {
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
