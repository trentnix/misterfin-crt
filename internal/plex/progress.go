package plex

import (
	"context"
	"errors"
	"net/url"
	"strconv"

	"mistervision/internal/media"
)

// playbackReports binds the duration to one prepared stream. Plex requires duration
// in timeline reports, while the shared playback state only carries position.
type playbackReports struct {
	client   *Client
	duration int64
}

// ReportPlaying sends timeline state, which also persists resume position.
// Stream release is independent of reporting success.
func (p playbackReports) ReportPlaying(ctx context.Context, event string, state media.PlayState) error {
	if !validID(state.ItemID) {
		return errors.New("invalid Plex playback item")
	}
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
	q := url.Values{"ratingKey": {state.ItemID}, "key": {"/library/metadata/" + state.ItemID}, "state": {status}, "time": {strconv.FormatInt(max(0, state.PositionTicks)/10000, 10)}, "duration": {strconv.FormatInt(max(0, p.duration)/10000, 10)}, "X-Plex-Session-Identifier": {state.PlaySessionID}}
	_, err := p.client.request(ctx, "POST", "/:/timeline", q)
	return err
}

// SavePlaybackPosition leaves resume persistence to timeline reports. Explicit
// completion also marks the item watched. It does not alter unrelated history.
func (p playbackReports) SavePlaybackPosition(ctx context.Context, item string, ticks int64, played bool) error {
	if !played {
		return nil
	}
	if !validID(item) {
		return errors.New("invalid Plex item ID")
	}
	_, err := p.client.request(ctx, "PUT", "/:/scrobble", url.Values{"key": {item}, "identifier": {"com.plexapp.plugins.library"}})
	return err
}

// stopTranscode releases only the resource allocated to this playback attempt.
// A missing session is already released, including failed stream startups.
func (c *Client) stopTranscode(ctx context.Context, session string) error {
	_, err := c.request(ctx, "GET", "/video/:/transcode/universal/stop", url.Values{"session": {session}, "X-Plex-Session-Identifier": {session}})
	var httpErr *HTTPError
	if errors.As(err, &httpErr) && httpErr.Status == 404 {
		return nil
	}
	return err
}
