package jellyfin

import (
	"context"
	"errors"
	"net/url"
	"strconv"
)

// ItemsQuery preserves the C baseline's collection-specific field costs.
func ItemsQuery(user, parent, collection string, start, limit int) url.Values {
	q := url.Values{"userId": {user}, "ParentId": {parent}, "SortBy": {"SortName"}, "SortOrder": {"Ascending"}, "Fields": {"ProductionYear,RunTimeTicks,ChildCount,RecursiveItemCount"}, "EnableUserData": {"true"}, "ImageTypeLimit": {"1"}, "EnableImageTypes": {"Primary,Backdrop"}, "StartIndex": {strconv.Itoa(max(0, start))}, "Limit": {strconv.Itoa(limit)}}
	switch collection {
	case "movies", "musicvideos":
		q.Set("Recursive", "true")
		kind := "Movie"
		if collection == "musicvideos" {
			kind = "MusicVideo"
		}
		q.Set("IncludeItemTypes", kind)
		q.Set("Fields", "ProductionYear,RunTimeTicks")
	case "music":
		q.Set("Fields", "ProductionYear,RunTimeTicks,ChildCount")
	case "homevideos", "mixed":
		q.Set("Fields", "ProductionYear,RunTimeTicks")
		q.Set("EnableUserData", "false")
		if collection == "homevideos" {
			q.Set("IncludeItemTypes", "Folder,PhotoAlbum,Video,Photo")
		} else {
			q.Set("IncludeItemTypes", "Folder,PhotoAlbum,Movie,Series,Season,Episode,Video,MusicVideo,Audio,MusicAlbum,MusicArtist,Photo,Book,AudioBook,BoxSet,Playlist,Trailer,Recording")
		}
	}
	return q
}

// List fetches and validates a page using the C client's endpoint-specific
// queries. It normalizes channel, season, and episode types. Views and seasons
// are returned as complete lists with totals derived from their item counts.
func (c *Client) List(ctx context.Context, loc Location, start, limit int) (Page, error) {
	path := "/Items"
	q := ItemsQuery(c.Session.UserID, loc.ParentID, loc.Collection, start, limit)
	switch loc.Kind {
	case "views":
		path = "/UserViews"
		q = url.Values{"userId": {c.Session.UserID}}
	case "seasons", "episodes":
		path = "/Shows/" + url.PathEscape(loc.SeriesID) + "/Seasons"
		q = url.Values{"userId": {c.Session.UserID}, "Fields": {"ChildCount"}, "ImageTypeLimit": {"1"}, "EnableImageTypes": {"Primary,Backdrop"}, "StartIndex": {strconv.Itoa(start)}, "Limit": {strconv.Itoa(limit)}}
		if loc.Kind == "episodes" {
			path = "/Shows/" + url.PathEscape(loc.SeriesID) + "/Episodes"
			q.Set("seasonId", loc.ParentID)
			q.Set("Fields", "RunTimeTicks")
			q.Set("EnableUserData", "true")
		} else {
			q = url.Values{"userId": {c.Session.UserID}}
		}
	case "livetv":
		path = "/LiveTv/Channels"
		q = url.Values{"userId": {c.Session.UserID}, "StartIndex": {strconv.Itoa(start)}, "Limit": {strconv.Itoa(limit)}, "AddCurrentProgram": {"true"}, "EnableImages": {"true"}, "ImageTypeLimit": {"1"}, "EnableImageTypes": {"Primary"}}
	}
	var p Page
	if err := c.json(ctx, "GET", path, q, nil, &p); err != nil {
		return p, err
	}
	if p.Items == nil || (p.TotalRecordCount != nil && *p.TotalRecordCount < 0) {
		return Page{}, errors.New("invalid Jellyfin item list")
	}
	for i := range p.Items {
		if p.Items[i].ID == "" {
			return Page{}, errors.New("Jellyfin item is missing its ID")
		}
		switch loc.Kind {
		case "livetv":
			p.Items[i].Type = "TvChannel"
		case "seasons":
			p.Items[i].Type = "Season"
		case "episodes":
			p.Items[i].Type = "Episode"
		}
		if loc.Kind == "views" && p.Items[i].CollectionType == "" {
			p.Items[i].CollectionType = "mixed"
		}
	}
	if loc.Kind == "views" || loc.Kind == "seasons" {
		total := len(p.Items)
		p.TotalRecordCount = &total
	}
	return p, nil
}

// Details fetches metadata, playback position, and image tags for one item.
// It rejects a response whose item ID does not match the requested ID.
func (c *Client) Details(ctx context.Context, id string) (Item, error) {
	var item Item
	err := c.json(ctx, "GET", "/Items/"+url.PathEscape(id), url.Values{"userId": {c.Session.UserID}, "Fields": {"Overview,ProductionYear,RunTimeTicks,People,MediaStreams,CommunityRating"}, "EnableUserData": {"true"}, "EnableImageTypes": {"Primary,Logo,Backdrop"}}, nil, &item)
	if err == nil && item.ID != id {
		err = errors.New("invalid item details")
	}
	return item, err
}

// CollectionItemType mirrors collection_item_type in src/jellyfin.c.
func CollectionItemType(collection string) string {
	return map[string]string{"movies": "Movie", "tvshows": "Series", "music": "MusicAlbum", "musicvideos": "MusicVideo", "homevideos": "Video,Photo", "mixed": "Movie,Series,Video,MusicVideo,Audio,Photo"}[collection]
}

// LibraryCount uses jf_count_items' query, independently of the cover sample.
// Live TV has no item count in the C carousel.
func (c *Client) LibraryCount(ctx context.Context, item Item) (*int, error) {
	if item.CollectionType == "livetv" {
		return nil, nil
	}
	q := url.Values{"userId": {c.Session.UserID}, "ParentId": {item.ID}, "Recursive": {"true"}, "Limit": {"0"}}
	if kind := CollectionItemType(item.CollectionType); kind != "" {
		q.Set("IncludeItemTypes", kind)
	}
	var page Page
	if err := c.json(ctx, "GET", "/Items", q, nil, &page); err != nil {
		return nil, err
	}
	if page.TotalRecordCount == nil || *page.TotalRecordCount < 0 {
		return nil, errors.New("library count unavailable")
	}
	return page.TotalRecordCount, nil
}

// Mosaic fetches at most twelve primary-artwork candidates for a library.
// It does not determine the library count. Live TV returns an empty page without
// making a request.
func (c *Client) Mosaic(ctx context.Context, item Item) (Page, error) {
	if item.CollectionType == "livetv" {
		return Page{}, nil
	}
	q := url.Values{"userId": {c.Session.UserID}, "ParentId": {item.ID}, "Recursive": {"true"}, "Limit": {"12"}, "SortBy": {"SortName"}, "SortOrder": {"Ascending"}, "Fields": {"ProductionYear,RunTimeTicks"}, "EnableUserData": {"true"}, "ImageTypeLimit": {"1"}, "EnableImageTypes": {"Primary"}}
	if kind := CollectionItemType(item.CollectionType); kind != "" {
		q.Set("IncludeItemTypes", kind)
	}
	var page Page
	err := c.json(ctx, "GET", "/Items", q, nil, &page)
	if len(page.Items) > 12 {
		page.Items = page.Items[:12]
	}
	return page, err
}

// Libraries preserves server names and order. Like C, it probes channels only
// when UserViews does not already contain a Live TV entry.
func (c *Client) Libraries(ctx context.Context) (Page, error) {
	page, err := c.List(ctx, Location{Kind: "views"}, 0, 0)
	if err != nil {
		return page, err
	}
	for _, item := range page.Items {
		if item.CollectionType == "livetv" {
			return page, nil
		}
	}
	channels, err := c.List(ctx, Location{Kind: "livetv"}, 0, 1)
	if err == nil && (len(channels.Items) > 0 || channels.TotalRecordCount != nil && *channels.TotalRecordCount > 0) {
		page.Items = append(page.Items, Item{ID: "misterfin-go:live-tv", Name: "Live TV", CollectionType: "livetv", IsFolder: true})
	}
	total := len(page.Items)
	page.TotalRecordCount = &total
	return page, nil
}
