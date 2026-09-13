package jellyfin

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// TrackSelection uses Jellyfin stream indexes, not positions in the stream list.
// AudioIndex -1 lets the server choose. SubtitleIndex -1 disables subtitles.
type TrackSelection struct{ AudioIndex, SubtitleIndex int }

// TextSubtitle reports whether Jellyfin can export a track as SubRip text.
// Unknown codecs use server burn-in, following the C client's fallback.
func (s MediaStream) TextSubtitle() bool {
	if s.Type != "Subtitle" {
		return false
	}
	switch strings.ToLower(s.Codec) {
	case "subrip", "srt", "ass", "ssa", "vtt", "webvtt", "mov_text", "microdvd", "sami", "smi", "ttml", "stl":
		return true
	}
	return false
}

// Label prefers the server's descriptive title, including language and codec.
func (s MediaStream) Label() string {
	label := s.DisplayTitle
	if label != "" && s.Title != "" && !strings.Contains(strings.ToLower(label), strings.ToLower(s.Title)) {
		label += " - " + s.Title
	}
	if label == "" {
		label = s.Title
	}
	if label == "" {
		label = strings.TrimSpace(s.Language + " " + strings.ToUpper(s.Codec))
	}
	if label == "" {
		label = s.Type + " " + strconv.Itoa(s.Index)
	}
	if s.IsForced && !strings.Contains(strings.ToLower(label), "forced") {
		label += " (forced)"
	}
	return label
}

// SelectedVideoURL adds explicit stream choices to the baseline transcode query.
// burnSubtitle is -1 for client-rendered text and disabled subtitles.
func (c *Client) SelectedVideoURL(itemID, sessionID string, start int64, ntsc bool, source string, selection TrackSelection, burnSubtitle int) string {
	u, _ := url.Parse(c.VideoStreamURL(itemID, sessionID, start, ntsc))
	q := u.Query()
	if source != "" {
		q.Set("mediaSourceId", source)
	}
	if selection.AudioIndex >= 0 {
		q.Set("audioStreamIndex", strconv.Itoa(selection.AudioIndex))
	}
	// Explicitly disable implicit server subtitles when Go owns their rendering.
	q.Set("subtitleStreamIndex", strconv.Itoa(burnSubtitle))
	if burnSubtitle >= 0 {
		q.Set("subtitleMethod", "Encode")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// Subtitle downloads bounded SubRip through the authenticated HTTP transport.
// Request errors omit the URL and token. Cancellation stops extraction/download.
func (c *Client) Subtitle(ctx context.Context, itemID, source string, index int) ([]byte, error) {
	if index < 0 {
		return nil, errors.New("invalid subtitle index")
	}
	if source == "" {
		source = itemID
	}
	path := "/Videos/" + url.PathEscape(itemID) + "/" + url.PathEscape(source) + "/Subtitles/" + strconv.Itoa(index) + "/Stream.srt"
	data, err := c.request(ctx, "GET", path, nil, nil)
	if err != nil {
		return nil, err
	}
	if len(data) > 4<<20 {
		return nil, errors.New("subtitle exceeds 4 MiB")
	}
	return data, nil
}

// PlaybackDetails also requests source IDs for subtitle extraction and transcoding.
// Ordinary browsing keeps the smaller baseline details query.
func (c *Client) PlaybackDetails(ctx context.Context, id string) (Item, error) {
	return c.details(ctx, id, ",MediaSources")
}
