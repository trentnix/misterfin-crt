package jellyfin

import (
	"context"
	"errors"
	"net/url"

	"misterfin-crt/internal/media"
)

// PlayState is Jellyfin's session-report payload. Shared playback supplies
// media.PlayState. ReportPlaying translates it and attaches remote queue data.
type PlayState struct {
	NowPlayingQueue     []QueueItem `json:",omitempty"`
	PlaylistItemID      string      `json:"PlaylistItemId,omitempty"`
	RepeatMode          string      `json:",omitempty"`
	PlaybackOrder       string      `json:",omitempty"`
	AudioStreamIndex    *int        `json:",omitempty"`
	SubtitleStreamIndex *int        `json:",omitempty"`
	ItemID              string      `json:"ItemId"`
	PlaySessionID       string      `json:"PlaySessionId"`
	MediaSourceID       string      `json:"MediaSourceId,omitempty"`
	LiveStreamID        string      `json:"LiveStreamId,omitempty"`
	CanSeek             *bool       `json:",omitempty"`
	Failed              *bool       `json:",omitempty"`
	PositionTicks       int64
	IsPaused            bool
	PlayMethod          string
}

// ReportPlaying sends start, progress, or stop state through the shared transport.
// It attaches queue metadata only when the reported media ID matches its owner.
func (c *Client) ReportPlaying(ctx context.Context, event string, state media.PlayState) error {
	return c.reportPlaying(ctx, event, state, "")
}

func (c *Client) reportPlaying(ctx context.Context, event string, facts media.PlayState, liveID string) error {
	state := PlayState{ItemID: facts.ItemID, PlaySessionID: facts.PlaySessionID, MediaSourceID: facts.MediaSourceID,
		LiveStreamID: liveID, AudioStreamIndex: facts.AudioStreamIndex, SubtitleStreamIndex: facts.SubtitleStreamIndex,
		CanSeek: facts.CanSeek, Failed: facts.Failed, PositionTicks: facts.PositionTicks, IsPaused: facts.IsPaused}
	if facts.Audio {
		state.PlayMethod = "DirectStream"
	}
	if q := c.queue.Load(); q != nil && q.ItemID == state.ItemID {
		state.NowPlayingQueue = q.Items
		state.PlaylistItemID = q.Current
		state.RepeatMode = q.RepeatMode
		state.PlaybackOrder = q.PlaybackOrder
	}
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

// SavePlaybackPosition stores resume position in 100-nanosecond ticks. Negative
// positions become zero. Marking an item played also clears its resume position.
// The call honors ctx and returns transport or server errors.
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
