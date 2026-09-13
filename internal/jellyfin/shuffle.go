package jellyfin

import (
	"context"
	"errors"
	"net/url"
)

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
