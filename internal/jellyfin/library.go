package jellyfin

import (
	"context"
	"errors"
	"net/url"
	"strconv"
)

// itemsQuery preserves collection-specific field costs and folder hierarchy.
func itemsQuery(user, parent, collection string, start, limit int) url.Values {
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
		}
		// Mixed libraries use their actual children without a type whitelist.
		// Including MusicArtist can pull unrelated artist entries into the
		// result on Jellyfin, despite ParentId. Explicit Folder filtering can
		// also expose the library's backing folder as an extra row.
	}
	return q
}

// List fetches and validates a page using the C client's endpoint-specific
// queries. It normalizes channel, season, and episode types. Views and seasons
// are returned as complete lists with totals derived from their item counts.
func (c *Client) List(ctx context.Context, loc Location, start, limit int) (Page, error) {
	path := "/Items"
	q := itemsQuery(c.Session.UserID, loc.ParentID, loc.Collection, start, limit)
	switch loc.Kind {
	case "views":
		path = "/UserViews"
		q = url.Values{"userId": {c.Session.UserID}}
	case "collections", "playlists":
		q = itemsQuery(c.Session.UserID, "", "", start, limit)
		q.Del("ParentId")
		q.Set("Recursive", "true")
		kind := "BoxSet"
		if loc.Kind == "playlists" {
			kind = "Playlist"
		}
		q.Set("IncludeItemTypes", kind)
	case "collection", "playlist":
		q = itemsQuery(c.Session.UserID, loc.ParentID, "", start, limit)
		q.Del("SortBy")
		q.Del("SortOrder")
		if loc.Kind == "playlist" {
			path = "/Playlists/" + url.PathEscape(loc.ParentID) + "/Items"
			q.Del("ParentId")
		}
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
	return c.details(ctx, id, "")
}

func (c *Client) details(ctx context.Context, id, extraFields string) (Item, error) {
	var item Item
	err := c.json(ctx, "GET", "/Items/"+url.PathEscape(id), url.Values{"userId": {c.Session.UserID}, "Fields": {"Overview,ProductionYear,RunTimeTicks,People,MediaStreams,CommunityRating" + extraFields}, "EnableUserData": {"true"}, "EnableImageTypes": {"Primary,Logo,Backdrop"}}, nil, &item)
	if err == nil && item.ID != id {
		err = errors.New("invalid item details")
	}
	return item, err
}

// collectionItemType selects types for library counts and mosaic sampling.
func collectionItemType(collection string) string {
	return map[string]string{"movies": "Movie", "tvshows": "Series", "music": "MusicAlbum", "musicvideos": "MusicVideo", "homevideos": "Video,Photo", "mixed": "Movie,Series,Video,MusicVideo,Audio,Photo"}[collection]
}

// LibraryCount uses jf_count_items' query, independently of the cover sample.
// Live TV has no item count in the C carousel.
func (c *Client) LibraryCount(ctx context.Context, item Item) (*int, error) {
	if item.CollectionType == "livetv" {
		return nil, nil
	}
	if loc, ok := organizationLocation(item); ok {
		page, err := c.List(ctx, loc, 0, 1)
		return page.TotalRecordCount, err
	}
	q := url.Values{"userId": {c.Session.UserID}, "ParentId": {item.ID}, "Recursive": {"true"}, "Limit": {"0"}}
	if kind := collectionItemType(item.CollectionType); kind != "" {
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
	if loc, ok := organizationLocation(item); ok {
		return c.List(ctx, loc, 0, 12)
	}
	q := url.Values{"userId": {c.Session.UserID}, "ParentId": {item.ID}, "Recursive": {"true"}, "Limit": {"12"}, "SortBy": {"SortName"}, "SortOrder": {"Ascending"}, "Fields": {"ProductionYear,RunTimeTicks"}, "EnableUserData": {"true"}, "ImageTypeLimit": {"1"}, "EnableImageTypes": {"Primary"}}
	if kind := collectionItemType(item.CollectionType); kind != "" {
		q.Set("IncludeItemTypes", kind)
	}
	var page Page
	err := c.json(ctx, "GET", "/Items", q, nil, &page)
	if len(page.Items) > 12 {
		page.Items = page.Items[:12]
	}
	return page, err
}

// Libraries preserves server names and order, then adds accessible collections,
// playlists, and Live TV when UserViews does not already contain those cards.
// Empty or unavailable organizational cards are omitted even when UserViews
// includes them. Optional catalog failures do not hide ordinary libraries.
func (c *Client) Libraries(ctx context.Context) (Page, error) {
	page, err := c.List(ctx, Location{Kind: "views"}, 0, 0)
	if err != nil {
		return page, err
	}
	present := make(map[string]bool)
	views := page.Items
	page.Items = make([]Item, 0, len(views)+3)
	for _, item := range views {
		present[item.CollectionType] = true
		if loc, ok := organizationLocation(item); ok {
			children, err := c.List(ctx, loc, 0, 1)
			if err != nil || len(children.Items) == 0 {
				continue
			}
		}
		page.Items = append(page.Items, item)
	}
	for _, card := range []Item{
		{ID: "mistervision:collections", Name: "Collections", CollectionType: "boxsets", IsFolder: true},
		{ID: "mistervision:playlists", Name: "Playlists", CollectionType: "playlists", IsFolder: true},
		{ID: "mistervision:live-tv", Name: "Live TV", CollectionType: "livetv", IsFolder: true},
	} {
		if present[card.CollectionType] {
			continue
		}
		loc, _ := organizationLocation(card)
		if card.CollectionType == "livetv" {
			loc.Kind = "livetv"
		}
		children, err := c.List(ctx, loc, 0, 1)
		if err == nil && len(children.Items) > 0 {
			page.Items = append(page.Items, card)
		}
	}
	if err := ctx.Err(); err != nil {
		return Page{}, err
	}
	total := len(page.Items)
	page.TotalRecordCount = &total
	return page, nil
}

// organizationLocation keeps synthetic card IDs out of Jellyfin parent filters.
func organizationLocation(item Item) (Location, bool) {
	switch item.CollectionType {
	case "boxsets":
		return Location{Kind: "collections"}, true
	case "playlists":
		return Location{Kind: "playlists"}, true
	}
	return Location{}, false
}
