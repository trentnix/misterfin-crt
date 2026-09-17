package jellyfin

import (
	"context"
	"errors"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"mistervision/internal/media"
)

// livePlayback holds the identifiers allocated by PlaybackInfo. StreamURL
// contains credentials and must never be logged or passed to a player.
type livePlayback struct {
	LiveStreamID, MediaSourceID, PlaySessionID, StreamURL string
	MediaStreams                                          []MediaStream
}

// liveProfile combines configured size/bitrate limits with the C codecs. The caller
// supplies the frame-rate cap to match its output cadence.
func (c *Client) liveProfile(maxFrameRate float64) any {
	profile := c.Config.transcodeProfile()
	conditions := []any{}
	for _, limit := range []struct {
		name  string
		value float64
	}{{"Width", float64(profile.MaxWidth)}, {"Height", float64(profile.MaxHeight)}, {"VideoFramerate", maxFrameRate}} {
		conditions = append(conditions, map[string]any{"Condition": "LessThanEqual", "Property": limit.name, "Value": strconv.FormatFloat(limit.value, 'f', -1, 64), "IsRequired": true})
	}
	return map[string]any{
		"UserId": c.Session.UserID, "StartTimeTicks": 0, "IsPlayback": true, "AutoOpenLiveStream": true,
		"EnableDirectPlay": false, "EnableDirectStream": false, "EnableTranscoding": true,
		"AllowVideoStreamCopy": false, "AllowAudioStreamCopy": false, "MaxStreamingBitrate": profile.VideoBitrate,
		"DeviceProfile": map[string]any{
			"Name": "MiSTerVision", "MaxStreamingBitrate": profile.VideoBitrate, "MaxStaticBitrate": profile.VideoBitrate,
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

// openLive negotiates a transcoded channel and returns its stream identity.
// maxFrameRate must be finite and positive. It caps conversion without forcing
// slower sources to a higher rate. Failed or canceled negotiation releases the tuner.
func (c *Client) openLive(ctx context.Context, channel string, maxFrameRate float64) (livePlayback, error) {
	if err := ctx.Err(); err != nil {
		return livePlayback{}, err
	}
	if maxFrameRate <= 0 || math.IsNaN(maxFrameRate) || math.IsInf(maxFrameRate, 0) {
		return livePlayback{}, errors.New("Live TV frame-rate limit must be finite and positive")
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
	var live livePlayback
	if len(response.MediaSources) > 0 {
		source := response.MediaSources[0]
		live = livePlayback{LiveStreamID: source.LiveStreamID, MediaSourceID: source.ID, PlaySessionID: response.PlaySessionID, MediaStreams: source.MediaStreams}
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
			_ = c.closeLive(cleanup, source.LiveStreamID)
		}
		return livePlayback{}, err
	}
	return live, nil
}

// closeLive releases a negotiated tuner. An empty identifier is a no-op.
// Callers must supply a live, bounded context during cancellation cleanup.
func (c *Client) closeLive(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	_, err := c.request(ctx, "POST", "/LiveStreams/Close", url.Values{"LiveStreamId": {id}}, nil)
	return err
}

// PrepareLive wraps Jellyfin's tuner negotiation in an owned stream. The tuner
// identity stays in this adapter for reporting and release.
func (c *Client) PrepareLive(ctx context.Context, request media.LiveRequest) (media.PreparedStream, error) {
	if request.AudioIndex != -1 {
		return media.PreparedStream{}, errors.New("Live TV audio selection is not available")
	}
	live, err := c.openLive(ctx, request.ChannelID, request.MaxFrameRate)
	if err != nil {
		return media.PreparedStream{}, err
	}
	return media.PreparedStream{URL: live.StreamURL, Limits: streamLimits(live.StreamURL), SessionID: live.PlaySessionID, SourceID: live.MediaSourceID, Streams: live.MediaStreams,
		Reports: liveReports{Client: c, id: live.LiveStreamID}, Release: func(ctx context.Context) error { return c.closeLive(ctx, live.LiveStreamID) }}, nil
}

// streamLimits reads bounded diagnostic values from a negotiated Live TV URL.
// Jellyfin may use different capitalization for query names.
func streamLimits(raw string) media.StreamLimits {
	u, err := url.Parse(raw)
	if err != nil {
		return media.StreamLimits{}
	}
	q := u.Query()
	number := func(key string) float64 {
		value := q.Get(key)
		if value == "" {
			for name, values := range q {
				if strings.EqualFold(name, key) && len(values) > 0 {
					value = values[0]
					break
				}
			}
		}
		n, _ := strconv.ParseFloat(value, 64)
		if n < 0 || n > 1e9 || n != n {
			return 0
		}
		return n
	}
	return media.StreamLimits{MaxWidth: number("maxWidth"), MaxHeight: number("maxHeight"), VideoBitrate: number("videoBitRate"), MaxFrameRate: number("maxFramerate")}
}

// liveReports binds tuner identity without exposing it to shared playback.
type liveReports struct {
	*Client
	id string
}

// ReportPlaying attaches the negotiated tuner ID to shared playback facts.
func (r liveReports) ReportPlaying(ctx context.Context, event string, state media.PlayState) error {
	return r.reportPlaying(ctx, event, state, r.id)
}
