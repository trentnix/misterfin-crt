package playback

import (
	"context"
	"errors"

	"misterfin-go/internal/jellyfin"
)

// preparePlayback resolves metadata, resume position, and Live TV negotiation.
// A nil session with no error means preparation was canceled.
func preparePlayback(ctx context.Context, c *jellyfin.Client, item jellyfin.Item, o Options) (*playbackSession, error) {
	liveTV := jellyfin.IsLive(item)
	item, err := c.Details(ctx, item.ID)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil
		}
		return nil, errors.New("cannot load playback details")
	}
	if liveTV {
		item.Type = "TvChannel"
	}
	liveTV = jellyfin.IsLive(item)
	if !Supported(item) {
		return nil, errors.New("playback for this item type is not implemented")
	}
	session, err := jellyfin.NewPlaySessionID()
	if err != nil {
		return nil, errors.New("cannot create playback session")
	}
	start := max(int64(0), item.UserData.PlaybackPositionTicks)
	if item.UserData.Played || liveTV || item.Type == "Audio" {
		start = 0
	}
	if o.StartTicks != nil && !liveTV && item.Type != "Audio" {
		start = max(int64(0), *o.StartTicks)
		if item.RunTimeTicks > 0 {
			start = min(start, max(int64(0), item.RunTimeTicks-10000000))
		}
	}
	streamURL := c.VideoStreamURL(item.ID, session, start, o.Height == 240 || o.Height == 480)
	if item.Type == "Audio" {
		streamURL = c.AudioStreamURL(item.ID, session)
	}
	var live jellyfin.LivePlayback
	if liveTV {
		live, err = c.OpenLive(ctx, item.ID, o.Height == 240 || o.Height == 480)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil
			}
			return nil, err
		}
		session, streamURL = live.PlaySessionID, live.StreamURL
		if len(live.MediaStreams) > 0 {
			item.MediaStreams = live.MediaStreams
		}
	}
	state := jellyfin.PlayState{ItemID: item.ID, PlaySessionID: session, PositionTicks: start}
	if item.Type == "Audio" {
		state.PlayMethod = "DirectStream"
	}
	if liveTV {
		canSeek := false
		state.MediaSourceID, state.LiveStreamID, state.CanSeek = live.MediaSourceID, live.LiveStreamID, &canSeek
	}
	return &playbackSession{
		client: c, item: item, start: start, streamURL: streamURL,
		live: live, liveTV: liveTV, state: state, played: item.UserData.Played,
	}, nil
}
