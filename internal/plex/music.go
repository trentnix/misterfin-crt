package plex

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"mistervision/internal/media"
)

// RandomTracks returns up to 64 unique tracks sampled from one music library.
// Each request starts a new draw, so tracks may repeat across batches.
func (c *Client) RandomTracks(ctx context.Context, library string) ([]media.Item, error) {
	section, ok := sectionID(library)
	if !ok {
		return nil, errors.New("invalid Plex music library")
	}
	page, err := c.page(ctx, "/library/sections/"+section+"/all", url.Values{"type": {"10"}, "sort": {"random"}}, 0, 64)
	if err != nil {
		return nil, err
	}
	tracks := make([]media.Item, 0, min(64, len(page.Items)))
	seen := make(map[string]bool)
	for _, item := range page.Items {
		if item.Type != "Audio" {
			return nil, errors.New("invalid Plex shuffle track")
		}
		if !seen[item.ID] && len(tracks) < 64 {
			tracks = append(tracks, item)
			seen[item.ID] = true
		}
	}
	return tracks, nil
}

// AudioQueue returns tracks from an entire listing in server order. All returned
// rows count toward the 10,000-item limit. Errors return no partial queue.
func (c *Client) AudioQueue(ctx context.Context, location media.Location) ([]media.Item, error) {
	var tracks []media.Item
	for start := 0; ; {
		page, err := c.List(ctx, location, start, 200)
		if err != nil {
			return nil, err
		}
		start += len(page.Items)
		if start > 10000 {
			return nil, errors.New("queue exceeds 10000 items")
		}
		for _, item := range page.Items {
			if item.Type == "Audio" {
				tracks = append(tracks, item)
			}
		}
		if len(page.Items) == 0 || (page.TotalRecordCount != nil && start >= *page.TotalRecordCount) || (page.TotalRecordCount == nil && len(page.Items) < 200) {
			return tracks, nil
		}
	}
}

// PrepareAudio opens the original single-file track through the authenticated
// stream proxy. The decoder can request byte ranges without receiving a token.
func (c *Client) PrepareAudio(ctx context.Context, item media.Item, session string) (media.PreparedStream, error) {
	if session == "" {
		return media.PreparedStream{}, errors.New("missing Plex playback session")
	}
	entry, err := c.metadata(ctx, item.ID)
	if err != nil {
		return media.PreparedStream{}, err
	}
	if entry.Type != "track" || len(entry.Media) == 0 || len(entry.Media[0].Part) != 1 {
		return media.PreparedStream{}, errors.New("Plex audio source unavailable")
	}
	path := entry.Media[0].Part[0].Key
	if !strings.HasPrefix(path, "/library/parts/") || strings.ContainsAny(path, "?#\\") {
		return media.PreparedStream{}, errors.New("invalid Plex audio path")
	}
	return media.PreparedStream{URL: c.Config.Server + path + "?" + url.Values{"X-Plex-Session-Identifier": {session}}.Encode(), SessionID: session, Reports: playbackReports{client: c, duration: entry.Duration * 10000}}, nil
}
