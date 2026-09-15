package playback

import (
	"context"
	"errors"

	"misterfin-crt/internal/jellyfin"
)

// preparePlayback resolves metadata, resume position, and Live TV negotiation.
// A nil session with no error means preparation was canceled.
func preparePlayback(ctx context.Context, c *jellyfin.Client, config Config, request Request, choices trackPreparation) (*playbackSession, error) {
	item := request.Item
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
	if request.StartTicks != nil && !liveTV && item.Type != "Audio" {
		start = max(int64(0), *request.StartTicks)
		if item.RunTimeTicks > 0 {
			start = min(start, max(int64(0), item.RunTimeTicks-10000000))
		}
	}
	streamURL := c.VideoStreamURL(item.ID, session, start, config.Height == 240 || config.Height == 480)
	if item.Type == "Audio" {
		streamURL = c.AudioStreamURL(item.ID, session)
	}
	var tracks VideoTracks
	if !liveTV && item.Type != "Audio" {
		tracks, err = videoTracks(item, choices)
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
		streamURL = c.SelectedVideoURL(item.ID, session, start, config.Height == 240 || config.Height == 480, tracks.SourceID, tracks.Selection, burn)
	}
	var live jellyfin.LivePlayback
	if liveTV {
		// Progressive NTSC keeps its 30 fps cap. Interlaced NTSC uses the
		// broadcast rate so 30 fps conversion does not periodically shorten
		// a frame against the core's approximately 59.94 Hz field clock.
		maxFrameRate := 25.0
		switch config.Height {
		case 240:
			maxFrameRate = 30
		case 480:
			maxFrameRate = 30000.0 / 1001
		}
		live, err = c.OpenLive(ctx, item.ID, maxFrameRate)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil
			}
			return nil, err
		}
		// Live tracks describe the negotiated source. Track switching and recorded
		// subtitle extraction remain unavailable, but picture fitting is local.
		tracks = VideoTracks{
			SourceID: live.MediaSourceID, Streams: live.MediaStreams, LivePicture: choices.livePicture,
			TrackOptions: TrackOptions{Picture: choices.picture(), Selection: jellyfin.TrackSelection{AudioIndex: -1, SubtitleIndex: -1}},
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
		preferences: config.Preferences, preferenceKey: preferenceKey(c, item.ID),
	}, nil
}
