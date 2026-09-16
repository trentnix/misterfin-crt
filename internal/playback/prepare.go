package playback

import (
	"context"
	"errors"

	"misterfin-crt/internal/media"
)

// preparePlayback resolves metadata, resume position, and Live TV negotiation.
// A nil session with no error means preparation was canceled.
func preparePlayback(ctx context.Context, c media.Playback, config Config, request Request, choices trackPreparation) (*playbackSession, error) {
	item := request.Item
	liveTV := media.IsLive(item)
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
	liveTV = media.IsLive(item)
	if !Supported(item) {
		return nil, errors.New("playback for this item type is not implemented")
	}
	session, err := media.NewPlaySessionID()
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
	var stream media.PreparedStream
	if item.Type == "Audio" {
		stream, err = c.PrepareAudio(ctx, item, session)
		if err != nil {
			return nil, err
		}
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
		stream, err = c.PrepareVideo(ctx, media.VideoRequest{Item: item, SessionID: session, StartTicks: start, NTSC: config.Height == 240 || config.Height == 480, SourceID: tracks.SourceID, Tracks: tracks.Selection, BurnSubtitle: burn})
		if err != nil {
			return nil, err
		}
	}
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
		live, ok := c.(media.LiveTV)
		if !ok {
			return nil, errors.New("Live TV is not supported by this server")
		}
		stream, err = live.PrepareLive(ctx, item.ID, maxFrameRate)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil
			}
			return nil, err
		}
		// Live tracks describe the negotiated source. Track switching and recorded
		// subtitle extraction remain unavailable, but picture fitting is local.
		tracks = VideoTracks{
			SourceID: stream.SourceID, Streams: stream.Streams, LivePicture: choices.livePicture,
			TrackOptions: TrackOptions{Picture: choices.picture(), Selection: media.TrackSelection{AudioIndex: -1, SubtitleIndex: -1}},
		}
		if len(stream.Streams) > 0 {
			item.MediaStreams = stream.Streams
		}
	}
	state := media.PlayState{ItemID: item.ID, PlaySessionID: stream.SessionID, PositionTicks: start}
	if !liveTV && item.Type != "Audio" {
		state.MediaSourceID = tracks.SourceID
		selection := tracks.Selection
		state.SubtitleStreamIndex = &selection.SubtitleIndex
		if selection.AudioIndex >= 0 {
			state.AudioStreamIndex = &selection.AudioIndex
		}
	}
	if item.Type == "Audio" {
		state.Audio = true
	}
	if liveTV {
		canSeek := false
		state.MediaSourceID, state.CanSeek = stream.SourceID, &canSeek
	}
	return &playbackSession{
		client: c, item: item, start: start,
		stream: stream, liveTV: liveTV, state: state, played: item.UserData.Played, tracks: tracks,
		preferences: config.Preferences, preferenceKey: preferenceKey(c, item.ID),
	}, nil
}
