package jellyfin

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// RemoteItems resolves an ordered remote request, including duplicate IDs.
// Containers expand to playable descendants, up to 10,000 items. The caller
// bounds duration through ctx. A failed lookup never returns a partial queue.
func (c *Client) RemoteItems(ctx context.Context, ids []string, mix bool) ([]Item, error) {
	if len(ids) == 0 || len(ids) > queueItemLimit {
		return nil, errors.New("invalid remote queue size")
	}
	if mix {
		return c.remotePage(ctx, "/Items/"+url.PathEscape(ids[0])+"/InstantMix", url.Values{"userId": {c.Session.UserID}})
	}
	var result []Item
	for start := 0; start < len(ids); start += queuePageSize {
		batch := ids[start:min(start+queuePageSize, len(ids))]
		var page Page
		q := url.Values{"userId": {c.Session.UserID}, "Ids": {strings.Join(batch, ",")}, "Fields": {"RunTimeTicks"}, "EnableUserData": {"true"}, "Limit": {strconv.Itoa(queuePageSize)}}
		if err := c.json(ctx, "GET", "/Items", q, nil, &page); err != nil {
			return nil, err
		}
		items := make(map[string]Item, len(page.Items))
		for _, item := range page.Items {
			items[item.ID] = item
		}
		for _, id := range batch {
			item, ok := items[id]
			if !ok {
				return nil, errors.New("remote item unavailable")
			}
			if item.IsFolder || item.Type == "MusicAlbum" || item.Type == "MusicArtist" || item.Type == "Playlist" || item.Type == "Series" || item.Type == "Season" {
				path := "/Items"
				query := url.Values{"userId": {c.Session.UserID}, "ParentId": {id}, "Recursive": {"true"}, "IncludeItemTypes": {"Audio,Movie,Episode,Video,MusicVideo"}, "SortBy": {"ParentIndexNumber,IndexNumber,SortName"}}
				if item.Type == "Playlist" {
					path = "/Playlists/" + url.PathEscape(id) + "/Items"
					query = url.Values{"userId": {c.Session.UserID}}
				}
				children, err := c.remotePage(ctx, path, query)
				if err != nil {
					return nil, err
				}
				result = append(result, children...)
			} else {
				result = append(result, item)
			}
			if len(result) > queueItemLimit {
				return nil, errors.New("remote queue exceeds 10000 items")
			}
		}
	}
	if len(result) == 0 {
		return nil, errors.New("remote queue is empty")
	}
	return result, nil
}

func (c *Client) remotePage(ctx context.Context, path string, q url.Values) ([]Item, error) {
	q.Set("Fields", "RunTimeTicks")
	return collectQueuePages(func(start, limit int) (Page, error) {
		q.Set("StartIndex", strconv.Itoa(start))
		q.Set("Limit", strconv.Itoa(limit))
		var page Page
		err := c.json(ctx, "GET", path, q, nil, &page)
		return page, err
	})
}
