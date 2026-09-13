package playback

import (
	"context"
	"errors"

	"misterfin-crt/internal/jellyfin"
)

// preparePlayback resolves metadata, resume position, and Live TV negotiation.
// A nil session with no error means preparation was canceled.
func preparePlayback(ctx context.Context, c *jellyfin.Client, item jellyfin.Item, o Options) (*playbackSession, error) {
	liveTV := jellyfin.IsLive(item)
	item, err := c.PlaybackDetails(ctx, item.ID)
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
	var tracks VideoTracks
	if !liveTV && item.Type != "Audio" {
		tracks, err = videoTracks(item, o)
		if err != nil {
			return nil, err
		}
		// Decoder geometry must describe the same source as the transcode request.
		item.MediaStreams = tracks.Streams
		burn := -1
		if sub, ok := tracks.Stream("Subtitle", tracks.Selection.SubtitleIndex); ok && (!sub.TextSubtitle() || !tracks.ClientSubtitles) {
			burn = sub.Index
			tracks.Text = nil
		}
		streamURL = c.SelectedVideoURL(item.ID, session, start, o.Height == 240 || o.Height == 480, tracks.SourceID, tracks.Selection, burn)
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
	if !liveTV && item.Type != "Audio" {
		state.MediaSourceID = tracks.SourceID
		selection := tracks.Selection
		state.SubtitleStreamIndex = &selection.SubtitleIndex
		if selection.AudioIndex >= 0 {
			state.AudioStreamIndex = &selection.AudioIndex
		}
	}
	if item.Type == "Audio" {
		state.PlayMethod = "DirectStream"
	}
	if liveTV {
		canSeek := false
		state.MediaSourceID, state.LiveStreamID, state.CanSeek = live.MediaSourceID, live.LiveStreamID, &canSeek
	}
	return &playbackSession{
		client: c, item: item, start: start, streamURL: streamURL,
		live: live, liveTV: liveTV, state: state, played: item.UserData.Played, tracks: tracks,
		preferences: o.Preferences, preferenceKey: preferenceKey(c, item.ID),
	}, nil
}
