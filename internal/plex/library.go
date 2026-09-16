package plex

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"misterfin-crt/internal/media"
)

// Libraries lists personal-media sections, then Live TV when accessible tuner
// channels exist. An unavailable optional tuner does not hide personal libraries.
func (c *Client) Libraries(ctx context.Context) (media.Page, error) {
	var response containerResponse
	if err := c.json(ctx, "/library/sections", nil, &response); err != nil {
		return media.Page{}, err
	}
	if response.Container == nil {
		return media.Page{}, errors.New("missing Plex library container")
	}
	result := media.Page{Items: []media.Item{}}
	for _, section := range response.Container.Directories {
		collection := map[string]string{"movie": "movies", "show": "tvshows", "artist": "music", "photo": "photos"}[section.Type]
		if collection == "" {
			continue
		}
		if !validID(section.Key) {
			return media.Page{}, errors.New("invalid Plex library ID")
		}
		result.Items = append(result.Items, media.Item{ID: "library:" + section.Key, Name: section.Title, Type: "CollectionFolder", CollectionType: collection, IsFolder: true})
	}
	if channels, err := c.liveChannels(ctx); err == nil && len(channels) > 0 {
		result.Items = append(result.Items, media.Item{ID: liveLibraryID, Name: "Live TV", CollectionType: "livetv", IsFolder: true})
	}
	if err := ctx.Err(); err != nil {
		return media.Page{}, err
	}
	total := len(result.Items)
	result.TotalRecordCount = &total
	return result, nil
}

// List maps shared navigation locations to sections or metadata children.
func (c *Client) List(ctx context.Context, loc media.Location, start, limit int) (media.Page, error) {
	if loc.Kind == "views" {
		return c.Libraries(ctx)
	}
	if loc.Kind == "livetv" {
		return c.channelPage(ctx, start, limit)
	}
	path := ""
	q := url.Values{}
	if section, ok := sectionID(loc.ParentID); ok {
		path = "/library/sections/" + section + "/all"
		q.Set("sort", "titleSort:asc")
	} else {
		id := loc.ParentID
		if loc.Kind == "seasons" {
			id = loc.SeriesID
		}
		if !validID(id) {
			return media.Page{}, errors.New("invalid Plex folder ID")
		}
		path = "/library/metadata/" + id + "/children"
	}
	return c.page(ctx, path, q, start, limit)
}

func (c *Client) page(ctx context.Context, path string, q url.Values, start, limit int) (media.Page, error) {
	if q == nil {
		q = url.Values{}
	}
	q.Set("X-Plex-Container-Start", strconv.Itoa(max(0, start)))
	q.Set("X-Plex-Container-Size", strconv.Itoa(max(1, min(limit, 200))))
	var response containerResponse
	if err := c.json(ctx, path, q, &response); err != nil {
		return media.Page{}, err
	}
	if response.Container == nil {
		return media.Page{}, errors.New("missing Plex item container")
	}
	container := response.Container
	if container.Total != nil && *container.Total < 0 {
		return media.Page{}, errors.New("invalid Plex item count")
	}
	result := media.Page{Items: []media.Item{}, TotalRecordCount: container.Total}
	entries := container.entries()
	for _, entry := range entries {
		if !validID(string(entry.ID)) {
			return media.Page{}, errors.New("invalid Plex item ID")
		}
		result.Items = append(result.Items, entry.item())
	}
	return result, nil
}

func (c *Client) metadata(ctx context.Context, id string) (metadata, error) {
	if !validID(id) {
		return metadata{}, errors.New("invalid Plex item ID")
	}
	var response containerResponse
	if err := c.json(ctx, "/library/metadata/"+id, nil, &response); err != nil {
		return metadata{}, err
	}
	if response.Container == nil {
		return metadata{}, errors.New("invalid Plex item details")
	}
	entries := response.Container.entries()
	if len(entries) != 1 || string(entries[0].ID) != id {
		return metadata{}, errors.New("invalid Plex item details")
	}
	return entries[0], nil
}

// Details returns one item's metadata, artwork references, and resume position.
func (c *Client) Details(ctx context.Context, id string) (media.Item, error) {
	if strings.HasPrefix(id, "live:") {
		return c.channelDetails(ctx, id)
	}
	entry, err := c.metadata(ctx, id)
	if err != nil {
		return media.Item{}, err
	}
	return entry.item(), nil
}

// PlaybackDetails also supplies source and stream IDs for playback selection.
func (c *Client) PlaybackDetails(ctx context.Context, id string) (media.Item, error) {
	return c.Details(ctx, id)
}

// LibraryCount reports the same top-level item count as opening the library.
func (c *Client) LibraryCount(ctx context.Context, item media.Item) (*int, error) {
	page, err := c.List(ctx, libraryLocation(item), 0, 1)
	return page.TotalRecordCount, err
}

// Mosaic obtains a small, deterministic cover sample for the library collage.
func (c *Client) Mosaic(ctx context.Context, item media.Item) (media.Page, error) {
	return c.List(ctx, libraryLocation(item), 0, 12)
}

// libraryLocation keeps synthetic Live TV out of personal-library endpoints.
func libraryLocation(item media.Item) media.Location {
	if item.CollectionType == "livetv" || item.ID == liveLibraryID {
		return media.Location{Kind: "livetv"}
	}
	return media.Location{Kind: "items", ParentID: item.ID}
}
