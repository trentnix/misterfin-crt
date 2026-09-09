package jellyfin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// VideoStreamURL follows jf_stream_url in the C baseline. Keep this URL private:
// the ApiKey query parameter authenticates Jellyfin's progressive stream.
func (c *Client) VideoStreamURL(itemID, sessionID string, start int64, ntsc bool) string {
	fps := "25"
	if ntsc {
		fps = "30"
	}
	q := url.Values{"static": {"false"}, "videoCodec": {"mpeg2video"}, "container": {"ts"}, "audioCodec": {"mp3"}, "audioChannels": {"2"}, "allowVideoStreamCopy": {"false"}, "audioSampleRate": {"48000"}, "maxWidth": {"720"}, "maxHeight": {"576"}, "videoBitRate": {"12000000"}, "maxFramerate": {fps}, "startTimeTicks": {strconv.FormatInt(max(0, start), 10)}, "playSessionId": {sessionID}, "deviceId": {c.Session.DeviceID}, "ApiKey": {c.Session.Token}}
	return c.Config.Server + "/Videos/" + url.PathEscape(itemID) + "/stream?" + q.Encode()
}

// AudioStreamURL matches jf_audio_stream_url: original audio, without transcoding.
func (c *Client) AudioStreamURL(itemID, sessionID string) string {
	q := url.Values{"static": {"true"}, "playSessionId": {sessionID}, "ApiKey": {c.Session.Token}}
	return c.Config.Server + "/Audio/" + url.PathEscape(itemID) + "/stream?" + q.Encode()
}

func NewPlaySessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "misterfin-go-" + hex.EncodeToString(b[:]), nil
}

// OpenVideo keeps HTTP and TLS in Go. Players receive only an anonymous pipe.
// Unlike JSON requests, media has no body-size or total-duration limit.
func (c *Client) OpenVideo(ctx context.Context, itemID, sessionID string, start int64, ntsc bool) (io.ReadCloser, error) {
	return c.OpenStream(ctx, c.VideoStreamURL(itemID, sessionID, start, ntsc))
}

// OpenStream accepts a private URL produced by VideoStreamURL or OpenLive.
func (c *Client) OpenStream(ctx context.Context, streamURL string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", streamURL, nil)
	if err != nil {
		return nil, errors.New("cannot create media request")
	}
	transport := c.HTTP.Transport
	if t, ok := transport.(*http.Transport); ok {
		clone := t.Clone()
		clone.ResponseHeaderTimeout = 15 * time.Second
		clone.DisableKeepAlives = true
		transport = clone
	}
	client := &http.Client{Transport: transport, CheckRedirect: c.HTTP.CheckRedirect}
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("cannot open Jellyfin media stream")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		return nil, &HTTPError{Status: response.StatusCode}
	}
	return response.Body, nil
}

type PlayState struct {
	ItemID        string `json:"ItemId"`
	PlaySessionID string `json:"PlaySessionId"`
	MediaSourceID string `json:"MediaSourceId,omitempty"`
	LiveStreamID  string `json:"LiveStreamId,omitempty"`
	CanSeek       *bool  `json:",omitempty"`
	Failed        *bool  `json:",omitempty"`
	PositionTicks int64
	IsPaused      bool
	PlayMethod    string
}

func (c *Client) ReportPlaying(ctx context.Context, event string, state PlayState) error {
	path := "/Sessions/Playing"
	switch event {
	case "start":
	case "progress":
		path += "/Progress"
	case "stopped":
		path += "/Stopped"
	default:
		return errors.New("invalid playback event")
	}
	if state.PlayMethod == "" {
		state.PlayMethod = "Transcode"
	}
	state.PositionTicks = max(0, state.PositionTicks)
	_, err := c.request(ctx, "POST", path, nil, state)
	return err
}
func (c *Client) SavePlaybackPosition(ctx context.Context, item string, ticks int64, played bool) error {
	if played {
		ticks = 0
	}
	body := struct {
		PlaybackPositionTicks int64
		Played                bool
	}{max(0, ticks), played}
	_, err := c.request(ctx, "POST", "/UserItems/"+url.PathEscape(item)+"/UserData", url.Values{"userId": {c.Session.UserID}}, body)
	return err
}
