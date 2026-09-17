package plex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"mistervision/internal/media"
)

const liveLibraryID = "plex:live-tv"

// dvr describes enabled channel mappings, not recording subscriptions. A channel
// may have several tuner devices. The browser exposes it once per DVR.
type dvr struct {
	EPGIdentifier string     `json:"epgIdentifier"`
	ID            identifier `json:"key"`
	Lineup        string     `json:"lineup"`
	// Plex also sends a capitalized Lineup array. Keep it distinct from the URI.
	Lineups json.RawMessage `json:"Lineup"`
	Devices []struct {
		ID       identifier `json:"key"`
		State    string
		Mappings []struct {
			ChannelKey, DeviceIdentifier, LineupIdentifier string
			Enabled                                        identifier
		} `json:"ChannelMapping"`
	} `json:"Device"`
}

type guideChannel struct{ Key, Identifier, Title, CallSign, Thumb string }
type deviceChannel struct {
	Identifier, Name string
	DRM              bool
}

// liveChannels reads enabled mappings and enriches them with optional guide and
// tuner names. A missing guide still leaves numbered channels usable.
func (c *Client) liveChannels(ctx context.Context) ([]media.Item, error) {
	var response struct {
		Container *struct {
			DVRs []dvr `json:"Dvr"`
		} `json:"MediaContainer"`
	}
	if err := c.json(ctx, "/livetv/dvrs", nil, &response); err != nil {
		return nil, err
	}
	if response.Container == nil {
		return nil, errors.New("missing Plex DVR container")
	}
	var items []media.Item
	seen := make(map[string]bool)
	for _, dvr := range response.Container.DVRs {
		if !validID(string(dvr.ID)) {
			return nil, errors.New("invalid Plex DVR ID")
		}
		guide := c.channelGuide(ctx, dvr.Lineup)
		programs := c.currentPrograms(ctx, dvr.EPGIdentifier)
		for _, device := range dvr.Devices {
			if device.State == "disabled" {
				continue
			}
			if !validID(string(device.ID)) {
				return nil, errors.New("invalid Plex tuner ID")
			}
			names := c.deviceChannels(ctx, string(device.ID))
			for _, mapping := range device.Mappings {
				if mapping.Enabled != "1" && mapping.Enabled != "true" {
					continue
				}
				key := mapping.ChannelKey
				if key == "" {
					key = mapping.LineupIdentifier
				}
				if key == "" {
					key = mapping.DeviceIdentifier
				}
				if !liveSegment(key) {
					return nil, errors.New("invalid Plex channel ID")
				}
				id := "live:" + string(dvr.ID) + ":" + base64.RawURLEncoding.EncodeToString([]byte(key))
				if seen[id] || names[mapping.DeviceIdentifier].DRM {
					continue
				}
				seen[id] = true
				detail := guide[mapping.ChannelKey]
				if detail.Identifier == "" {
					detail = guide[mapping.LineupIdentifier]
				}
				name := detail.CallSign
				if name == "" {
					name = names[mapping.DeviceIdentifier].Name
				}
				if name == "" {
					name = detail.Title
				}
				if name == "" {
					name = "Channel " + mapping.DeviceIdentifier
				}
				item := media.Item{ID: id, Name: name, Type: "TvChannel", Number: mapping.DeviceIdentifier, ImageTags: map[string]string{}}
				if item.Number == "" {
					item.Number = mapping.LineupIdentifier
				}
				item.CurrentProgram.Name = programs[mapping.ChannelKey]
				if detail.Thumb != "" {
					item.ImageTags["Primary"] = detail.Thumb
				}
				items = append(items, item)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Natural order keeps 2.2 before 2.10 without assuming all identifiers are numeric.
	slices.SortStableFunc(items, func(a, b media.Item) int { return compareChannelNumbers(a.Number, b.Number) })
	return items, nil
}

// channelGuide is optional. Guide errors cannot prevent tuning an enabled channel.
func (c *Client) channelGuide(ctx context.Context, lineup string) map[string]guideChannel {
	result := make(map[string]guideChannel)
	if lineup == "" {
		return result
	}
	var response struct {
		Container struct {
			Channels []guideChannel `json:"Channel"`
		} `json:"MediaContainer"`
	}
	if c.json(ctx, "/livetv/epg/channels", url.Values{"lineup": {lineup}}, &response) != nil {
		return result
	}
	for _, ch := range response.Container.Channels {
		result[ch.Key], result[ch.Identifier] = ch, ch
	}
	return result
}

// currentPrograms adds now-playing titles without making guide availability a
// requirement for tuning. Plex's grid filters apply to each airing's time range.
func (c *Client) currentPrograms(ctx context.Context, provider string) map[string]string {
	result := make(map[string]string)
	name, id, ok := strings.Cut(provider, ":")
	if !ok || !validID(id) || (name != "tv.plex.providers.epg.cloud" && name != "tv.plex.providers.epg.xmltv" && name != "tv.plex.providers.epg.custom") {
		return result
	}
	now := time.Now().Unix()
	q := url.Values{"type": {"1,4"}, "beginsAt<": {strconv.FormatInt(now, 10)}, "endsAt>": {strconv.FormatInt(now, 10)}, "X-Plex-Container-Size": {"2000"}}
	var response struct {
		Container struct {
			Metadata []struct {
				Title, GrandparentTitle string
				Media                   []struct {
					ChannelIdentifier string
					BeginsAt, EndsAt  int64
				}
			}
		} `json:"MediaContainer"`
	}
	if c.json(ctx, "/"+provider+"/grid", q, &response) != nil {
		return result
	}
	for _, item := range response.Container.Metadata {
		title := item.GrandparentTitle
		if title == "" {
			title = item.Title
		}
		for _, airing := range item.Media {
			if airing.BeginsAt <= now && airing.EndsAt > now {
				result[airing.ChannelIdentifier] = title
			}
		}
	}
	return result
}

// deviceChannels supplies broadcast names and identifies protected channels.
func (c *Client) deviceChannels(ctx context.Context, id string) map[string]deviceChannel {
	result := make(map[string]deviceChannel)
	var response struct {
		Container struct {
			Channels []deviceChannel `json:"DeviceChannel"`
		} `json:"MediaContainer"`
	}
	if c.json(ctx, "/media/grabbers/devices/"+id+"/channels", nil, &response) != nil {
		return result
	}
	for _, ch := range response.Container.Channels {
		result[ch.Identifier] = ch
	}
	return result
}

// channelPage applies shared paging limits to the deduplicated lineup.
func (c *Client) channelPage(ctx context.Context, start, limit int) (media.Page, error) {
	items, err := c.liveChannels(ctx)
	if err != nil {
		return media.Page{}, err
	}
	total := len(items)
	start = min(max(0, start), total)
	end := min(total, start+max(1, min(limit, 200)))
	return media.Page{Items: items[start:end], TotalRecordCount: &total}, nil
}

// channelDetails resolves current names and rejects channels removed since browsing.
func (c *Client) channelDetails(ctx context.Context, id string) (media.Item, error) {
	if _, _, err := parseChannelID(id); err != nil {
		return media.Item{}, err
	}
	items, err := c.liveChannels(ctx)
	if err != nil {
		return media.Item{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return media.Item{}, errors.New("Plex channel is no longer available")
}

// parseChannelID decodes an opaque browser identity into safe endpoint segments.
func parseChannelID(id string) (string, string, error) {
	parts := strings.Split(id, ":")
	if len(parts) == 3 && parts[0] == "live" && validID(parts[1]) {
		raw, err := base64.RawURLEncoding.DecodeString(parts[2])
		if err == nil && liveSegment(string(raw)) {
			return parts[1], string(raw), nil
		}
	}
	return "", "", errors.New("invalid Plex channel ID")
}

// liveSegment excludes path traversal and controls while allowing XMLTV names.
func liveSegment(value string) bool {
	if value == "" || len(value) > 512 || value == "." || value == ".." || !utf8.ValidString(value) || strings.ContainsAny(value, "/\\?#%") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// compareChannelNumbers compares digit runs without integer overflow.
func compareChannelNumbers(a, b string) int {
	for len(a) > 0 && len(b) > 0 {
		if a[0] >= '0' && a[0] <= '9' && b[0] >= '0' && b[0] <= '9' {
			ai, bi := 0, 0
			for ai < len(a) && a[ai] >= '0' && a[ai] <= '9' {
				ai++
			}
			for bi < len(b) && b[bi] >= '0' && b[bi] <= '9' {
				bi++
			}
			an, bn := strings.TrimLeft(a[:ai], "0"), strings.TrimLeft(b[:bi], "0")
			if len(an) != len(bn) {
				return len(an) - len(bn)
			}
			if n := strings.Compare(an, bn); n != 0 {
				return n
			}
			a, b = a[ai:], b[bi:]
		} else {
			if a[0] != b[0] {
				return int(a[0]) - int(b[0])
			}
			a, b = a[1:], b[1:]
		}
	}
	return len(a) - len(b)
}
