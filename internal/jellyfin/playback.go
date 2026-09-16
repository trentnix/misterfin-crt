package jellyfin

import (
	"context"
	"errors"
	"net/url"
	"strconv"

	"misterfin-crt/internal/media"
)

// PrepareVideo builds one progressive MPEG-2 request with explicit track choices.
// The URL contains credentials and stays inside the authenticated stream source.
func (c *Client) PrepareVideo(ctx context.Context, request media.VideoRequest) (media.PreparedStream, error) {
	if err := ctx.Err(); err != nil {
		return media.PreparedStream{}, err
	}
	profile := c.Config.transcodeProfile()
	fps := 25
	if request.NTSC {
		fps = 30
	}
	q := url.Values{
		"static": {"false"}, "videoCodec": {"mpeg2video"}, "container": {"ts"},
		"audioCodec": {"mp3"}, "audioChannels": {"2"}, "audioSampleRate": {"48000"},
		"allowVideoStreamCopy": {"false"},
		"maxWidth":             {strconv.Itoa(profile.MaxWidth)}, "maxHeight": {strconv.Itoa(profile.MaxHeight)},
		"videoBitRate": {strconv.Itoa(profile.VideoBitrate)}, "maxFramerate": {strconv.Itoa(fps)},
		"startTimeTicks": {strconv.FormatInt(max(0, request.StartTicks), 10)},
		"playSessionId":  {request.SessionID}, "deviceId": {c.Session.DeviceID}, "ApiKey": {c.Session.Token},
	}
	if request.SourceID != "" {
		q.Set("mediaSourceId", request.SourceID)
	}
	if request.Tracks.AudioIndex >= 0 {
		q.Set("audioStreamIndex", strconv.Itoa(request.Tracks.AudioIndex))
	}
	// Disable implicit server subtitles when the client owns their rendering.
	q.Set("subtitleStreamIndex", strconv.Itoa(request.BurnSubtitle))
	if request.BurnSubtitle >= 0 {
		q.Set("subtitleMethod", "Encode")
	}
	return media.PreparedStream{
		URL:       c.Config.Server + "/Videos/" + url.PathEscape(request.Item.ID) + "/stream?" + q.Encode(),
		SessionID: request.SessionID, SourceID: request.SourceID, Reports: c,
		Limits: media.StreamLimits{MaxWidth: float64(profile.MaxWidth), MaxHeight: float64(profile.MaxHeight), VideoBitrate: float64(profile.VideoBitrate), MaxFrameRate: float64(fps)},
	}, nil
}

// PrepareAudio returns original audio without applying video transcode settings.
func (c *Client) PrepareAudio(ctx context.Context, item media.Item, session string) (media.PreparedStream, error) {
	if err := ctx.Err(); err != nil {
		return media.PreparedStream{}, err
	}
	q := url.Values{"static": {"true"}, "playSessionId": {session}, "ApiKey": {c.Session.Token}}
	return media.PreparedStream{
		URL:       c.Config.Server + "/Audio/" + url.PathEscape(item.ID) + "/stream?" + q.Encode(),
		SessionID: session, Reports: c,
	}, nil
}

// PlaybackDetails requests source IDs for subtitle extraction and transcoding.
// Ordinary browsing keeps the smaller details query.
func (c *Client) PlaybackDetails(ctx context.Context, id string) (Item, error) {
	return c.details(ctx, id, ",MediaSources")
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
