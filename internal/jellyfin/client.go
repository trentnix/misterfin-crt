package jellyfin

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Item struct {
	ID                                             string `json:"Id"`
	Name, Type, CollectionType, SeriesID, Overview string
	IsFolder                                       bool
	ProductionYear                                 int
	RunTimeTicks                                   int64
	ChildCount, RecursiveItemCount                 int
	CommunityRating                                float64
	BackdropImageTags                              []string
	ImageTags                                      map[string]string
	ParentBackdropItemId                           string
	ParentBackdropImageTags                        []string
	Number, ChannelNumber                          string
	CurrentProgram                                 struct{ Name string }
	UserData                                       struct {
		Played                bool
		PlaybackPositionTicks int64
	}
}

type Page struct {
	Items            []Item
	TotalRecordCount *int
}

type Location struct{ Kind, ParentID, Collection, SeriesID string }

type Client struct {
	Config  Config
	Session Session
	HTTP    *http.Client
}

func NewClient(c Config, s Session) *Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: c.InsecureTLS}
	h := &http.Client{Timeout: 15 * time.Second, Transport: t, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != via[0].URL.Scheme || req.URL.Host != via[0].URL.Host {
			return errors.New("redirect outside server refused")
		}
		return nil
	}}
	return &Client{Config: c, Session: s, HTTP: h}
}

type HTTPError struct{ Status int }

func (e *HTTPError) Error() string { return fmt.Sprintf("Jellyfin returned HTTP %d", e.Status) }
func Rejected(err error) bool {
	var e *HTTPError
	return errors.As(err, &e) && (e.Status == 401 || e.Status == 403)
}

func (c *Client) request(ctx context.Context, method, path string, query url.Values, body any) ([]byte, error) {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	u := c.Config.Server + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(encoded))
	if err != nil {
		return nil, errors.New("invalid request URL")
	}
	// Quote all header values so saved credentials cannot inject header fields.
	auth := `MediaBrowser Client="MiSTerFin-Go", Device="MiSTerFin-Go", Version="0.1", DeviceId=` + strconv.Quote(c.Session.DeviceID)
	if c.Session.Token != "" {
		auth += ", Token=" + strconv.Quote(c.Session.Token)
	}
	req.Header.Set("Authorization", auth)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("cannot reach Jellyfin (check address, TLS certificate, and connection)")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &HTTPError{resp.StatusCode}
	}
	const limit = 8 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, errors.New("cannot read Jellyfin response")
	}
	if len(data) > limit {
		return nil, errors.New("Jellyfin response exceeds 8 MiB")
	}
	return data, nil
}

func (c *Client) json(ctx context.Context, method, path string, q url.Values, body, out any) error {
	b, err := c.request(ctx, method, path, q, body)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, out); err != nil {
		return errors.New("invalid Jellyfin JSON response")
	}
	return nil
}

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

func (c *Client) Image(ctx context.Context, item Item) (image.Image, error) {
	return c.ImageKind(ctx, item, "Primary")
}
func (c *Client) ImageKind(ctx context.Context, item Item, kind string) (image.Image, error) {
	tag := item.ImageTags[kind]
	if kind == "Backdrop" && len(item.BackdropImageTags) == 0 && len(item.ParentBackdropImageTags) > 0 {
		item.ID = item.ParentBackdropItemId
		item.BackdropImageTags = item.ParentBackdropImageTags
	}
	if kind == "Backdrop" && len(item.BackdropImageTags) > 0 {
		tag = item.BackdropImageTags[0]
	}
	if tag == "" {
		return nil, nil
	}
	format := "Jpg"
	if kind == "Logo" {
		format = "Png"
	}
	path := kind
	if kind == "Backdrop" {
		path += "/0"
	}
	width := "320"
	if kind == "Backdrop" {
		width = "640"
	}
	if kind == "Logo" {
		width = "480"
	}
	b, err := c.request(ctx, "GET", "/Items/"+url.PathEscape(item.ID)+"/Images/"+path, url.Values{"tag": {tag}, "maxWidth": {width}, "maxHeight": {"360"}, "quality": {"80"}, "format": {format}}, nil)
	if err != nil {
		return nil, err
	}
	conf, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || conf.Width < 1 || conf.Height < 1 || conf.Width > 2048 || conf.Height > 2048 {
		return nil, errors.New("invalid or oversized artwork")
	}
	im, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	// Bound cached pixels even when a server ignores the requested dimensions.
	maxWidth, _ := strconv.Atoi(width)
	scale := min(1.0, min(float64(maxWidth)/float64(conf.Width), 360.0/float64(conf.Height)))
	if scale < 1 {
		resized := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(conf.Width)*scale)), max(1, int(float64(conf.Height)*scale))))
		bounds := im.Bounds()
		for y := 0; y < resized.Bounds().Dy(); y++ {
			for x := 0; x < resized.Bounds().Dx(); x++ {
				resized.Set(x, y, im.At(bounds.Min.X+x*bounds.Dx()/resized.Bounds().Dx(), bounds.Min.Y+y*bounds.Dy()/resized.Bounds().Dy()))
			}
		}
		im = resized
	}
	return im, nil
}

// Authenticate preserves saved tokens on temporary failures. Only an explicit
// 401/403 starts replacement sign-in. The caller owns all client mutations.
func (c *Client) Authenticate(ctx context.Context, dir string, showCode func(string)) error {
	if c.Session.Token != "" && c.Session.UserID != "" {
		_, err := c.List(ctx, Location{Kind: "views"}, 0, 1)
		if err == nil {
			return nil
		}
		if !Rejected(err) {
			return err
		}
		c.Session.Token = ""
		c.Session.UserID = ""
	} else if c.Config.APIKey != "" {
		c.Session.Token = c.Config.APIKey
		var users []Item
		if err := c.json(ctx, "GET", "/Users", nil, nil, &users); err != nil {
			return err
		}
		for _, u := range users {
			if strings.EqualFold(u.Name, c.Config.Username) {
				c.Session.UserID = u.ID
				break
			}
		}
		if c.Session.UserID == "" {
			return errors.New("configured username was not found")
		}
		return nil
	}
	c.Session.Token = ""
	var enabled bool
	if err := c.json(ctx, "GET", "/QuickConnect/Enabled", nil, nil, &enabled); err != nil {
		return err
	}
	if !enabled {
		return errors.New("Quick Connect is disabled; configure an API key and username")
	}
	var qc struct {
		Secret, Code  string
		Authenticated bool
	}
	if err := c.json(ctx, "POST", "/QuickConnect/Initiate", nil, nil, &qc); err != nil {
		return err
	}
	if qc.Secret == "" || qc.Code == "" {
		return errors.New("invalid Quick Connect response")
	}
	showCode(qc.Code)
	deadline := time.NewTimer(5 * time.Minute)
	defer deadline.Stop()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("Quick Connect expired; press R to retry")
		case <-ticker.C:
		}
		var result struct{ Authenticated bool }
		if err := c.json(ctx, "GET", "/QuickConnect/Connect", url.Values{"secret": {qc.Secret}}, nil, &result); err != nil {
			return err
		}
		if !result.Authenticated {
			continue
		}
		var login struct {
			AccessToken string
			User        Item
		}
		if err := c.json(ctx, "POST", "/Users/AuthenticateWithQuickConnect", nil, map[string]string{"Secret": qc.Secret}, &login); err != nil {
			return err
		}
		if login.AccessToken == "" || login.User.ID == "" {
			return errors.New("invalid sign-in response")
		}
		c.Session.Token = login.AccessToken
		c.Session.UserID = login.User.ID
		if err := SaveSession(dir, c.Session); err != nil {
			return errors.New("signed in but cannot save Go session")
		}
		return nil
	}
}

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
