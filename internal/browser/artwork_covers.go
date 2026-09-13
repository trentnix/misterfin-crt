package browser

import (
	"context"
	"sync"

	"misterfin-go/internal/jellyfin"
)

// coverImages loads a resolved sample with at most three workers. Each completed
// image can render independently. Slots retain sample order. The caller must not
// mutate items until this method returns. emit may run concurrently.
func (l *artworkLoader) coverImages(ctx context.Context, items []jellyfin.Item, emit func(artUpdate)) {
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
