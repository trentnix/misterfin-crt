package plex

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"mistervision/internal/media"
)

var _ media.Server = (*Client)(nil)

// PrepareVideo chooses the source's streams and requests a progressive H.264
// transcode in Matroska. Go opens the authenticated stream and pipes it to the
// existing decoder. The offset is in seconds on Plex and ticks in shared state.
func (c *Client) PrepareVideo(ctx context.Context, request media.VideoRequest) (media.PreparedStream, error) {
	item, session, start, ntsc, source, selection, burn := request.Item, request.SessionID, request.StartTicks, request.NTSC, request.SourceID, request.Tracks, request.BurnSubtitle
	var streams []media.MediaStream
	valid := false
	for _, candidate := range item.MediaSources {
		if candidate.ID == source {
			valid = true
			streams = candidate.MediaStreams
			break
		}
	}
	if !valid || session == "" {
		return media.PreparedStream{}, errors.New("Plex video source unavailable")
	}
	if selection.SubtitleIndex >= 0 || burn >= 0 {
		found := false
		for _, track := range streams {
			if track.Type == "Subtitle" && track.Index == selection.SubtitleIndex {
				found = (burn == track.Index) || (burn < 0 && track.ClientSubtitle())
				break
			}
		}
		if !found {
			return media.PreparedStream{}, errors.New("Plex subtitle source unavailable")
		}
	}
	index, partID, ok := strings.Cut(source, ":")
	if !ok || !validID(index) || !validID(partID) || !validID(item.ID) {
		return media.PreparedStream{}, errors.New("invalid Plex media source")
	}
	q := url.Values{"subtitleStreamID": {"0"}}
	if burn >= 0 {
		q.Set("subtitleStreamID", strconv.Itoa(burn))
	}
	if selection.AudioIndex >= 0 {
		q.Set("audioStreamID", strconv.Itoa(selection.AudioIndex))
	}
	if _, err := c.request(ctx, "PUT", "/library/parts/"+partID, q); err != nil {
		return media.PreparedStream{}, err
	}
	fps := 25.0
	if ntsc {
		fps = 30
	}
	q, limits := c.videoQuery("/library/metadata/"+item.ID, session, fps)
	q.Set("mediaIndex", index)
	q.Set("offset", strconv.FormatFloat(float64(max(0, start))/10000000, 'f', 7, 64))
	if burn >= 0 {
		q.Set("subtitles", "burn")
		q.Set("advancedSubtitles", "burn")
		q.Set("subtitleSize", "100")
	}
	// Register the playback identity before opening the progressive stream.
	// Without this decision request Plex rejects an explicit session identity.
	// Omitting the identity lets an old timeline stop terminate a replacement
	// stream during seeking, even when the transcode session IDs differ.
	if err := c.decideVideo(ctx, q); err != nil {
		return media.PreparedStream{}, err
	}
	return media.PreparedStream{
		URL:       c.Config.Server + "/video/:/transcode/universal/start.mkv?" + q.Encode(),
		SessionID: session, SourceID: source,
		Reports: playbackReports{client: c, duration: item.RunTimeTicks},
		Limits:  limits,
		Release: func(ctx context.Context) error { return c.stopTranscode(ctx, session) }}, nil
}

// RequestStream sends media and byte-range requests only to the configured
// server. It has a header timeout but no total stream-duration timeout.
func (c *Client) RequestStream(ctx context.Context, method, raw string, headers http.Header) (*http.Response, error) {
	target, err := url.Parse(raw)
	origin, originErr := url.Parse(c.Config.Server)
	if err != nil || originErr != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") || target.Scheme != origin.Scheme || target.Host != origin.Host || target.User != nil || target.Fragment != "" || (method != "GET" && method != "HEAD") {
		return nil, errors.New("invalid Plex stream URL")
	}
	req, err := http.NewRequestWithContext(ctx, method, raw, nil)
	if err != nil {
		return nil, errors.New("invalid Plex stream request")
	}
	c.headers(req, c.Session.Token)
	req.Header.Set("Accept-Encoding", "identity")
	for _, name := range []string{"Range", "If-Range"} {
		if value := headers.Get(name); value != "" {
			req.Header.Set(name, value)
		}
	}
	transport := c.HTTP.Transport
	if original, ok := transport.(*http.Transport); ok {
		clone := original.Clone()
		clone.ResponseHeaderTimeout = 60 * time.Second
		clone.DisableKeepAlives = true
		transport = clone
	}
	client := &http.Client{Transport: transport, CheckRedirect: sameOrigin}
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("cannot open Plex media stream")
	}
	return response, nil
}

// OpenStream opens a progressive media body. The caller owns its close.
func (c *Client) OpenStream(ctx context.Context, raw string) (io.ReadCloser, error) {
	response, err := c.RequestStream(ctx, "GET", raw, nil)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		return nil, &HTTPError{response.StatusCode}
	}
	return response.Body, nil
}
