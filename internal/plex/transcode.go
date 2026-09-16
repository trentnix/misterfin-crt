package plex

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"misterfin-crt/internal/media"
)

// videoQuery applies one transcode policy to recorded video and tuner streams.
// Callers add their source offset and ownership identifiers before negotiation.
func (c *Client) videoQuery(path, session string, fps float64) (url.Values, media.StreamLimits) {
	width, height, bitrate := c.Config.MaxWidth, c.Config.MaxHeight, c.Config.VideoBitrate
	if width == 0 {
		width = 720
	}
	if height == 0 {
		height = 576
	}
	if bitrate == 0 {
		bitrate = 12000000
	}
	// Plex does not supply the MPEG-2 encoder used by the Jellyfin adapter.
	profile := "add-transcode-target(type=videoProfile&context=streaming&protocol=http&container=mkv&videoCodec=h264&audioCodec=mp3&replace=true)" +
		"+add-limitation(scope=videoCodec&scopeName=h264&type=upperBound&name=video.frameRate&value=" + strconv.FormatFloat(fps, 'f', -1, 64) + "&replace=true)"
	q := url.Values{
		"path":                        {path},
		"mediaIndex":                  {"0"},
		"partIndex":                   {"0"},
		"protocol":                    {"http"},
		"directPlay":                  {"0"},
		"directStream":                {"0"},
		"directStreamAudio":           {"0"},
		"fastSeek":                    {"1"},
		"location":                    {"lan"},
		"offset":                      {"0"},
		"videoResolution":             {fmt.Sprintf("%dx%d", width, height)},
		"videoBitrate":                {strconv.Itoa(bitrate / 1000)},
		"maxVideoBitrate":             {strconv.Itoa(bitrate / 1000)},
		"audioBoost":                  {"100"},
		"session":                     {session},
		"X-Plex-Session-Identifier":   {session},
		"X-Plex-Client-Profile-Name":  {"Generic"},
		"X-Plex-Client-Profile-Extra": {profile},
		"subtitles":                   {"none"}}
	return q, media.StreamLimits{MaxWidth: float64(width), MaxHeight: float64(height), VideoBitrate: float64(bitrate), MaxFrameRate: fps}
}

// decideVideo registers the playback identity before opening the stream.
func (c *Client) decideVideo(ctx context.Context, q url.Values) error {
	var decision struct {
		Container *struct {
			Code int `json:"generalDecisionCode"`
		} `json:"MediaContainer"`
	}
	if err := c.json(ctx, "/video/:/transcode/universal/decision", q, &decision); err != nil {
		return err
	}
	if decision.Container == nil || decision.Container.Code < 1000 || decision.Container.Code >= 2000 {
		return errors.New("Plex cannot convert this video")
	}
	return nil
}
