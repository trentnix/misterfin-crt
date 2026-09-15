package jellyfin

import (
	"context"
	"errors"
)

const (
	queuePageSize  = 200
	queueItemLimit = 10000
)

// AudioQueue returns the audio tracks from an entire listing in server order.
// It uses the same queries as List and counts every returned row toward the
// 10,000-item limit before filtering. Errors return no partial queue. The caller
// controls request lifetime through ctx.
func (c *Client) AudioQueue(ctx context.Context, location Location) ([]Item, error) {
	items, err := collectQueuePages(func(start, limit int) (Page, error) {
		return c.List(ctx, location, start, limit)
	})
	if err != nil {
		return nil, err
	}
	tracks := items[:0]
	for _, item := range items {
		if item.Type == "Audio" {
			tracks = append(tracks, item)
		}
	}
	clear(items[len(tracks):])
	return tracks, nil
}

// collectQueuePages applies one paging and size policy to album, playlist, and
// remote container expansion. Offsets count server rows, not filtered tracks.
func collectQueuePages(fetch func(start, limit int) (Page, error)) ([]Item, error) {
	var items []Item
	for {
		page, err := fetch(len(items), queuePageSize)
		if err != nil {
			return nil, err
		}
		if len(items)+len(page.Items) > queueItemLimit {
			return nil, errors.New("queue exceeds 10000 items")
		}
		items = append(items, page.Items...)
		if len(page.Items) == 0 || (page.TotalRecordCount != nil && len(items) >= *page.TotalRecordCount) || (page.TotalRecordCount == nil && len(page.Items) < queuePageSize) {
			return items, nil
		}
	}
}
