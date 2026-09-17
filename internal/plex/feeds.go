package plex

import (
	"context"

	"mistervision/internal/media"
)

// ContinueWatching uses Plex's combined resume and next-episode feed.
func (c *Client) ContinueWatching(ctx context.Context) (media.Page, error) {
	page, err := c.page(ctx, "/hubs/continueWatching/items", nil, 0, 100)
	if err != nil {
		return media.Page{}, err
	}
	items := page.Items[:0]
	for _, item := range page.Items {
		if item.Type == "Movie" || item.Type == "Episode" {
			items = append(items, item)
		}
	}
	page.Items = items
	for i := range page.Items {
		item := &page.Items[i]
		item.ContinueAction = "next"
		if item.UserData.PlaybackPositionTicks > 0 {
			item.ContinueAction = "resume"
		}
	}
	total := len(page.Items)
	page.TotalRecordCount = &total
	return page, nil
}
