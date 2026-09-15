package jellyfin

import (
	"context"
	"errors"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// LivePlayback holds the identifiers allocated by PlaybackInfo. StreamURL
// contains credentials and must never be logged or passed to a player.
type LivePlayback struct {
	LiveStreamID, MediaSourceID, PlaySessionID, StreamURL string
	MediaStreams                                          []MediaStream
}

func IsLive(item Item) bool {
	return item.Type == "TvChannel" || item.Type == "LiveTvChannel"
}

// liveProfile retains the C client's codec and size limits. The caller
// supplies the frame-rate cap to match its output cadence.
func (c *Client) liveProfile(maxFrameRate float64) any {
	conditions := []any{}
	for _, limit := range []struct {
		name  string
		value float64
	}{{"Width", 720}, {"Height", 576}, {"VideoFramerate", maxFrameRate}} {
		conditions = append(conditions, map[string]any{"Condition": "LessThanEqual", "Property": limit.name, "Value": strconv.FormatFloat(limit.value, 'f', -1, 64), "IsRequired": true})
	}
	return map[string]any{
		"UserId": c.Session.UserID, "StartTimeTicks": 0, "IsPlayback": true, "AutoOpenLiveStream": true,
		"EnableDirectPlay": false, "EnableDirectStream": false, "EnableTranscoding": true,
		"AllowVideoStreamCopy": false, "AllowAudioStreamCopy": false, "MaxStreamingBitrate": 12000000,
		"DeviceProfile": map[string]any{
			"Name": "MiSTerFin", "MaxStreamingBitrate": 12000000, "MaxStaticBitrate": 12000000,
			"DirectPlayProfiles": []any{}, "SubtitleProfiles": []any{},
			"TranscodingProfiles": []any{map[string]any{"Container": "ts", "Type": "Video", "Protocol": "http", "AudioCodec": "mp3", "VideoCodec": "mpeg2video", "Context": "Streaming", "MaxAudioChannels": "2"}},
			"CodecProfiles":       []any{map[string]any{"Type": "Video", "Codec": "mpeg2video", "Conditions": conditions}},
		},
	}
}

func (c *Client) liveURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || u.IsAbs() || u.Host != "" || u.Fragment != "" {
		return "", errors.New("invalid Live TV transcode URL")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", errors.New("invalid Live TV transcode query")
	}
	hasKey := false
	for key := range q {
		// Match the C workaround for Jellyfin's incomplete MPEG-2 level/profile pair.
		if strings.EqualFold(key, "level") || strings.EqualFold(key, "mpeg2video-level") {
			q.Del(key)
		}
		if strings.EqualFold(key, "ApiKey") {
			hasKey = true
		}
	}
	if !hasKey {
		q.Set("ApiKey", c.Session.Token)
	}
	u.RawQuery = q.Encode()
	return c.Config.Server + u.String(), nil
}

// OpenLive negotiates a transcoded channel and returns its stream identity.
// maxFrameRate must be finite and positive. It caps conversion without forcing
// slower sources to a higher rate. Failed or canceled negotiation releases the tuner.
func (c *Client) OpenLive(ctx context.Context, channel string, maxFrameRate float64) (LivePlayback, error) {
	if err := ctx.Err(); err != nil {
		return LivePlayback{}, err
	}
	if maxFrameRate <= 0 || math.IsNaN(maxFrameRate) || math.IsInf(maxFrameRate, 0) {
		return LivePlayback{}, errors.New("Live TV frame-rate limit must be finite and positive")
	}
	// Let negotiation finish after a user cancellation so we can learn and close
	// the tuner ID. The HTTP client and this context both bound the wait.
	negotiation, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	var response struct {
		PlaySessionID string `json:"PlaySessionId"`
		MediaSources  []struct {
			ID             string `json:"Id"`
			LiveStreamID   string `json:"LiveStreamId"`
			TranscodingURL string `json:"TranscodingUrl"`
			MediaStreams   []MediaStream
		}
	}
	err := c.json(negotiation, "POST", "/Items/"+url.PathEscape(channel)+"/PlaybackInfo", nil, c.liveProfile(maxFrameRate), &response)
	var live LivePlayback
	if len(response.MediaSources) > 0 {
		source := response.MediaSources[0]
		live = LivePlayback{LiveStreamID: source.LiveStreamID, MediaSourceID: source.ID, PlaySessionID: response.PlaySessionID, MediaStreams: source.MediaStreams}
		if err == nil {
			live.StreamURL, err = c.liveURL(source.TranscodingURL)
		}
	}
	if err == nil && (live.LiveStreamID == "" || live.MediaSourceID == "" || live.PlaySessionID == "" || live.StreamURL == "") {
		err = errors.New("Jellyfin did not provide a playable Live TV stream")
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		for _, source := range response.MediaSources {
			_ = c.CloseLive(cleanup, source.LiveStreamID)
		}
		return LivePlayback{}, err
	}
	return live, nil
}

func (c *Client) CloseLive(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	_, err := c.request(ctx, "POST", "/LiveStreams/Close", url.Values{"LiveStreamId": {id}}, nil)
	return err
}
