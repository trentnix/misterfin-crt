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

const artworkBudget = 16 * 1024 * 1024
const libraryCacheLimit = 32
const libraryCacheTTL = time.Minute

type imageKey struct{ id, kind, tag string }
type cachedImage struct {
	image image.Image
	bytes int
	used  uint64
}
type cachedLibrary struct {
	count      *int
	countUntil time.Time
	items      []jellyfin.Item
	itemsUntil time.Time
	used       uint64
}
type artUpdate struct {
	kind        string
	image       image.Image
	detail      *jellyfin.Item
	count       *int
	slot, total int
	err         error
}

// artworkLoader caches images by server identity and tag, independently of
// screens. Its lifetime is one authenticated browser session.
type artworkLoader struct {
	client                  *jellyfin.Client
	photoWidth, photoHeight int
	slots                   chan struct{}
	mu                      sync.Mutex
	images                  map[imageKey]cachedImage
	libraries               map[string]cachedLibrary
	bytes                   int
	clock                   uint64
}

func newArtworkLoader(client *jellyfin.Client) *artworkLoader {
	return &artworkLoader{client: client, photoWidth: 640, photoHeight: 288, slots: make(chan struct{}, 3), images: make(map[imageKey]cachedImage), libraries: make(map[string]cachedLibrary)}
}
func artworkKey(item jellyfin.Item, kind string) imageKey {
	key := imageKey{item.ID, kind, item.ImageTags[kind]}
	if kind == "Photo" {
		key.tag = item.ImageTags["Primary"]
	}
	if kind == "Backdrop" {
		if len(item.BackdropImageTags) > 0 {
			key.tag = item.BackdropImageTags[0]
		} else if len(item.ParentBackdropImageTags) > 0 {
			key.id = item.ParentBackdropItemId
			key.tag = item.ParentBackdropImageTags[0]
		}
	}
	return key
}
func (l *artworkLoader) cached(key imageKey) image.Image {
	l.mu.Lock()
	defer l.mu.Unlock()
	value, ok := l.images[key]
	if !ok {
		return nil
	}
	l.clock++
	value.used = l.clock
	l.images[key] = value
	return value.image
}
func (l *artworkLoader) remember(key imageKey, im image.Image) {
	if im == nil {
		return
	}
	size := im.Bounds().Dx() * im.Bounds().Dy() * 4
	if size > artworkBudget {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if old, ok := l.images[key]; ok {
		l.bytes -= old.bytes
		delete(l.images, key)
	}
	for l.bytes+size > artworkBudget || len(l.images) >= 128 {
		var oldest imageKey
		age := ^uint64(0)
		for k, v := range l.images {
			if v.used < age {
				oldest = k
				age = v.used
			}
		}
		l.bytes -= l.images[oldest].bytes
		delete(l.images, oldest)
	}
	l.clock++
	l.images[key] = cachedImage{im, size, l.clock}
	l.bytes += size
}
func (l *artworkLoader) library(id string) cachedLibrary {
	l.mu.Lock()
	defer l.mu.Unlock()
	value, ok := l.libraries[id]
	if ok {
		l.clock++
		value.used = l.clock
		l.libraries[id] = value
	}
	return value
}
func (l *artworkLoader) rememberLibrary(id string, update func(*cachedLibrary)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.libraries[id]; !ok && len(l.libraries) >= libraryCacheLimit {
		oldest := ""
		age := ^uint64(0)
		for k, v := range l.libraries {
			if v.used < age {
				oldest = k
				age = v.used
			}
		}
		delete(l.libraries, oldest)
	}
	value := l.libraries[id]
	update(&value)
	l.clock++
	value.used = l.clock
	l.libraries[id] = value
}
func (l *artworkLoader) forget(item jellyfin.Item) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.libraries, item.ID)
	for key, value := range l.images {
		if key.id == item.ID || item.ParentBackdropItemId != "" && key.id == item.ParentBackdropItemId {
			l.bytes -= value.bytes
			delete(l.images, key)
		}
	}
}
func (l *artworkLoader) snapshot(item jellyfin.Item, root bool) Artwork {
	if root {
		lib := l.library(item.ID)
		art := Artwork{}
		if time.Now().Before(lib.countUntil) {
			art.Count = lib.count
		}
		if time.Now().Before(lib.itemsUntil) {
			art.Covers = make([]image.Image, len(lib.items))
			for i, item := range lib.items {
				art.Covers[i] = l.cached(artworkKey(item, "Primary"))
			}
		}
		return art
	}
	return Artwork{Photo: l.cached(artworkKey(item, "Photo")), Primary: l.cached(artworkKey(item, "Primary")), Backdrop: l.cached(artworkKey(item, "Backdrop")), Logo: l.cached(artworkKey(item, "Logo"))}
}
func (l *artworkLoader) fetchImage(ctx context.Context, item jellyfin.Item, kind string) (image.Image, error) {
	key := artworkKey(item, kind)
	if key.tag == "" {
		if kind == "Photo" {
			return nil, errors.New("photo unavailable")
		}
		return nil, nil
	}
	if im := l.cached(key); im != nil {
		return im, nil
	}
	select {
	case l.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-l.slots }()
	if im := l.cached(key); im != nil {
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
		l.remember(key, im)
	}
	return im, err
}
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

// load sends each result independently. Metadata and counts have no selection
// debounce. Expensive image requests are debounced and limited to three at once.
func (l *artworkLoader) load(ctx context.Context, item jellyfin.Item, root, detail bool, emit func(artUpdate)) {
	if detail && item.Type == "Photo" {
		im, err := l.fetchImage(ctx, item, "Photo")
		if ctx.Err() == nil {
			emit(artUpdate{kind: "Photo", image: im, err: err})
		}
		return
	}
	if root {
		if item.CollectionType == "livetv" {
			return
		}
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			lib := l.library(item.ID)
			if time.Now().Before(lib.countUntil) {
				emit(artUpdate{kind: "count", count: lib.count})
				return
			}
			count, err := l.client.LibraryCount(ctx, item)
			if ctx.Err() != nil {
				return
			}
			if err == nil {
				l.rememberLibrary(item.ID, func(value *cachedLibrary) { value.count = count; value.countUntil = time.Now().Add(libraryCacheTTL) })
			}
			emit(artUpdate{kind: "count", count: count, err: err})
		}()
		defer wg.Wait()
		if !artworkDelay(ctx) {
			return
		}
		lib := l.library(item.ID)
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
			l.rememberLibrary(item.ID, func(value *cachedLibrary) { value.items = items; value.itemsUntil = time.Now().Add(libraryCacheTTL) })
		}
		// Workers take the next cover as soon as one completes. No goroutine per
		// library item is needed, and completed covers retain their sample order.
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

func applyArtwork(art *Artwork, update artUpdate) {
	switch update.kind {
	case "Photo":
		art.Photo = update.image
	case "count":
		art.Count = update.count
	case "Primary":
		art.Primary = update.image
	case "Backdrop":
		art.Backdrop = update.image
	case "Logo":
		art.Logo = update.image
	case "cover":
		if len(art.Covers) != update.total {
			art.Covers = make([]image.Image, update.total)
		}
		if update.slot >= 0 && update.slot < len(art.Covers) {
			art.Covers[update.slot] = update.image
		}
	}
}
