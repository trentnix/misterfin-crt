package browser

import (
	"context"
	"sync"

	"misterfin-crt/internal/jellyfin"
)

// itemImages requests each image independently and waits for all workers.
// emit may run concurrently and must return promptly.
func (l *selectionLoader) itemImages(ctx context.Context, item jellyfin.Item, detail bool, emit func(artUpdate)) {
	kinds := []string{"Primary", "Backdrop"}
	if detail {
		kinds = append(kinds, "Logo")
	}
	var wg sync.WaitGroup
	for _, kind := range kinds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			im, err := l.artwork.Fetch(ctx, item, kind)
			if ctx.Err() == nil {
				emit(artUpdate{kind: kind, image: im, err: err})
			}
		}()
	}
	wg.Wait()
}

// coverImages loads a resolved sample with at most three workers. Each completed
// image can render independently. Slots retain sample order. The caller must not
// mutate items until this method returns. emit may run concurrently.
func (l *selectionLoader) coverImages(ctx context.Context, items []jellyfin.Item, emit func(artUpdate)) {
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
				im, err := l.artwork.Fetch(ctx, items[i], "Primary")
				if ctx.Err() == nil {
					emit(artUpdate{kind: "cover", image: im, slot: i, total: len(items), err: err})
				}
			}
		}()
	}
}
