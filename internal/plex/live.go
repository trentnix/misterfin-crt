package plex

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"strconv"
	"time"

	"mistervision/internal/media"
)

var _ media.LiveTV = (*Client)(nil)

// livePlayback owns one consumer of a tuner and its separate video conversion.
// Plex can share the underlying broadcast with other clients or DVR recordings.
// Cleanup targets only this attempt's grab operation and transcode session.
type livePlayback struct {
	client                  *Client
	session, operation, key string
}

// liveVideo accepts both current nested tune responses and the flat response
// documented by Plex. Live metadata differs from library metadata: aspectRatio
// can be a string and stream indexes are not persistent library stream IDs.
type liveVideo struct {
	Media []struct {
		UUID          string
		AspectRatio   json.Number
		Width, Height int
		Part          []struct {
			ID      identifier
			Streams []struct {
				ID                  identifier
				Type                int `json:"streamType"`
				Index               int
				Codec, Language     string
				Title, DisplayTitle string
				Selected            bool
				Width, Height       int
			} `json:"Stream"`
		} `json:"Part"`
	} `json:"Media"`
}

// PrepareLive tunes a channel at the live edge using the same conversion limits
// as recorded video. Tuner identities and reporting remain inside this adapter.
// A canceled or failed preparation releases the consumer before returning.
func (c *Client) PrepareLive(ctx context.Context, request media.LiveRequest) (prepared media.PreparedStream, resultErr error) {
	if err := ctx.Err(); err != nil {
		return prepared, err
	}
	dvr, channel, err := parseChannelID(request.ChannelID)
	if err != nil {
		return prepared, err
	}
	frameRate := request.MaxFrameRate
	if request.AudioIndex < -1 {
		return prepared, errors.New("invalid Live TV audio selection")
	}
	if frameRate <= 0 || frameRate > 120 || math.IsNaN(frameRate) || math.IsInf(frameRate, 0) {
		return prepared, errors.New("invalid Plex Live TV frame-rate limit")
	}
	session, err := media.NewPlaySessionID()
	if err != nil {
		return prepared, err
	}
	live := livePlayback{client: c, session: session, operation: "/media/grabbers/operations/" + url.PathEscape(channel+"-"+session)}
	defer func() {
		if resultErr != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = live.release(cleanup)
		}
	}()
	// Finish bounded negotiation after cancellation so Plex cannot create a tuner
	// consumer after its cleanup. The operation ID is also known before the POST,
	// allowing cleanup after malformed replies or lost responses.
	negotiation, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	data, err := live.tune(negotiation, dvr, channel)
	if ctx.Err() != nil {
		return prepared, ctx.Err()
	}
	if err != nil {
		return prepared, err
	}
	video, err := tunedVideo(data)
	if err != nil {
		return prepared, err
	}
	liveAudio, err := c.selectLiveAudio(ctx, video, request.AudioIndex, live.identity())
	if err != nil {
		return prepared, err
	}
	source := video.Media[0]
	live.key = "/livetv/sessions/" + source.UUID
	q, limits := c.videoQuery(live.key, session, frameRate)
	q.Set("X-Plex-Client-Identifier", session)
	q.Set("offset", "-1")
	q.Set("hasMDE", "1")
	if err := c.decideVideo(ctx, q); err != nil {
		return prepared, err
	}
	var streams []media.MediaStream
	for _, part := range source.Part {
		for _, s := range part.Streams {
			kind := map[int]string{1: "Video", 2: "Audio", 3: "Subtitle"}[s.Type]
			if kind == "" {
				continue
			}
			track := media.MediaStream{Type: kind, Index: s.Index, Codec: s.Codec, Language: s.Language, Title: s.Title, DisplayTitle: s.DisplayTitle, IsDefault: s.Selected, Width: s.Width, Height: s.Height}
			if kind == "Video" && (track.Width == 0 || track.Height == 0) {
				track.Width, track.Height = source.Width, source.Height
			}
			if kind == "Video" {
				if aspect, err := source.AspectRatio.Float64(); err == nil && aspect > 0 && aspect < 100 {
					track.AspectRatio = strconv.FormatFloat(aspect, 'f', -1, 64)
				}
			}
			streams = append(streams, track)
		}
	}
	return media.PreparedStream{URL: c.Config.Server + "/video/:/transcode/universal/start.mkv?" + q.Encode(), SessionID: session, SourceID: source.UUID, Streams: streams, LiveAudio: liveAudio, Limits: limits, Reports: live, Release: live.release}, nil
}

// tune uses the negotiation deadline instead of the metadata client's shorter
// timeout. Plex Web also allows 30 seconds for tuner startup. The caller must
// provide a bounded context and release this consumer after a failed request.
func (p livePlayback) tune(ctx context.Context, dvr, channel string) ([]byte, error) {
	client := *p.client.HTTP
	client.Timeout = 0
	started := time.Now()
	data, status, err := p.client.fetch(ctx, &client, p.client.Config.Server, p.client.Session.Token,
		"POST", "/livetv/dvrs/"+dvr+"/channels/"+url.PathEscape(channel)+"/tune", p.identity())
	// Log the operation without server, channel, or consumer identifiers.
	p.client.Diagnostics.Request("POST", "/livetv/dvrs/:dvr/channels/:channel/tune", status, time.Since(started), int64(len(data)), err != nil)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, errors.New("Plex did not finish tuning this channel within 30 seconds. Try again")
	}
	return data, err
}

// tunedVideo normalizes the two tune envelopes without trusting returned URLs.
func tunedVideo(data []byte) (liveVideo, error) {
	var response struct {
		Container *struct {
			Status        int
			Metadata      []liveVideo
			Subscriptions []struct {
				Operations []struct {
					Metadata *liveVideo
					Video    *liveVideo
				} `json:"MediaGrabOperation"`
			} `json:"MediaSubscription"`
		} `json:"MediaContainer"`
	}
	if json.Unmarshal(data, &response) != nil || response.Container == nil {
		return liveVideo{}, errors.New("invalid Plex tune response")
	}
	c := response.Container
	if c.Status < 0 {
		return liveVideo{}, errors.New("Plex could not tune this channel; check the tuner and antenna")
	}
	videos := c.Metadata
	for _, sub := range c.Subscriptions {
		for _, operation := range sub.Operations {
			if operation.Metadata != nil {
				videos = append(videos, *operation.Metadata)
			} else if operation.Video != nil {
				videos = append(videos, *operation.Video)
			}
		}
	}
	for _, video := range videos {
		if len(video.Media) > 0 && validLiveSession(video.Media[0].UUID) {
			return video, nil
		}
	}
	return liveVideo{}, errors.New("Plex did not provide a playable Live TV session")
}

// validLiveSession accepts only the UUID characters used in live-session paths.
func validLiveSession(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-') {
			return false
		}
	}
	return true
}

// identity binds all tuner, stream, and reporting requests to this consumer.
func (p livePlayback) identity() url.Values {
	return url.Values{"X-Plex-Session-Identifier": {p.session}, "X-Plex-Client-Identifier": {p.session}}
}

// ReportPlaying keeps this live consumer active without modifying library history.
func (p livePlayback) ReportPlaying(ctx context.Context, event string, state media.PlayState) error {
	status := "playing"
	switch event {
	case "start", "progress":
		if state.IsPaused {
			status = "paused"
		}
	case "stopped":
		status = "stopped"
	default:
		return errors.New("invalid playback event")
	}
	q := p.identity()
	q.Set("key", p.key)
	q.Set("state", status)
	q.Set("time", strconv.FormatInt(max(0, state.PositionTicks)/10000, 10))
	_, err := p.client.request(ctx, "POST", "/:/timeline", q)
	return err
}

// SavePlaybackPosition is a no-op because live channels have no resume history.
func (p livePlayback) SavePlaybackPosition(context.Context, string, int64, bool) error { return nil }

// release stops conversion and detaches only this consumer's grab. A recording
// or another client on the same channel retains its own operation.
func (p livePlayback) release(ctx context.Context) error {
	q := p.identity()
	q.Set("session", p.session)
	results := make(chan error, 2)
	// Both resources receive the caller's cleanup deadline. A slow conversion
	// stop must not consume the entire budget before the tuner can be released.
	go func() {
		_, err := p.client.request(ctx, "GET", "/video/:/transcode/universal/stop", q)
		results <- alreadyReleased(err)
	}()
	go func() {
		_, err := p.client.request(ctx, "DELETE", p.operation, p.identity())
		results <- alreadyReleased(err)
	}()
	return errors.Join(<-results, <-results)
}

// alreadyReleased treats a missing resource as successful idempotent cleanup.
func alreadyReleased(err error) error {
	var response *HTTPError
	if errors.As(err, &response) && response.Status == 404 {
		return nil
	}
	return err
}
