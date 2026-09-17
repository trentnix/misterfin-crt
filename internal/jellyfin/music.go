package jellyfin

import (
	"context"
	"errors"
	"net/url"
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

// RandomTracks returns a bounded random batch from one music library. Each
// request starts a new draw, so tracks may repeat across batches.
func (c *Client) RandomTracks(ctx context.Context, library string) ([]Item, error) {
	q := url.Values{"userId": {c.Session.UserID}, "ParentId": {library}, "Recursive": {"true"}, "IncludeItemTypes": {"Audio"}, "SortBy": {"Random"}, "Fields": {"ProductionYear,RunTimeTicks"}, "EnableUserData": {"true"}, "ImageTypeLimit": {"1"}, "EnableImageTypes": {"Primary"}, "Limit": {"64"}}
	var page Page
	if err := c.json(ctx, "GET", "/Items", q, nil, &page); err != nil {
		return nil, err
	}
	if page.Items == nil {
		return nil, errors.New("invalid shuffle response")
	}
	items := make([]Item, 0, min(64, len(page.Items)))
	seen := make(map[string]bool)
	for _, item := range page.Items {
		if item.ID == "" || item.Type != "Audio" {
			return nil, errors.New("invalid shuffle track")
		}
		if !seen[item.ID] && len(items) < 64 {
			items = append(items, item)
			seen[item.ID] = true
		}
	}
	return items, nil
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
