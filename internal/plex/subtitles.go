package plex

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// Subtitle downloads a selected sidecar text track as UTF-8 SubRip. Embedded
// subtitles use burn-in, as advertised by the track's RequiresBurnIn flag.
// Item, source, and stream must agree before the authenticated download starts.
func (c *Client) Subtitle(ctx context.Context, itemID, source string, index int) ([]byte, error) {
	if index < 0 {
		return nil, errors.New("invalid Plex subtitle index")
	}
	entry, err := c.metadata(ctx, itemID)
	if err != nil {
		return nil, err
	}
	valid := false
	for _, track := range entry.item().MediaSources {
		if track.ID != source {
			continue
		}
		for _, stream := range track.MediaStreams {
			if stream.Index == index && stream.ClientSubtitle() && stream.IsExternal {
				valid = true
			}
		}
	}
	if !valid {
		return nil, errors.New("Plex subtitle source unavailable")
	}
	// Construct the local endpoint from a validated stream ID. A server-supplied
	// sidecar URL must never redirect private credentials to another host.
	data, err := c.request(ctx, "GET", "/library/streams/"+strconv.Itoa(index), url.Values{"format": {"srt"}, "encoding": {"utf-8"}})
	if err != nil {
		return nil, err
	}
	if len(data) > 4<<20 {
		return nil, errors.New("subtitle exceeds 4 MiB")
	}
	if !strings.Contains(string(data), "-->") {
		return nil, errors.New("Plex returned no timed subtitle text")
	}
	return data, nil
}
