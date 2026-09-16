package playback

import (
	"context"
	"errors"

	"misterfin-crt/internal/media"
	"misterfin-crt/internal/subtitles"
)

// TrackOptions preserves streams, downloaded text, and picture mode across handoffs.
// Text is immutable. Without saved or explicit choices, playback uses default
// audio, no subtitles, and Original.
type TrackOptions struct {
	Selection media.TrackSelection
	Text      *subtitles.Track
	Picture   PictureMode
}

// VideoTracks describes the source and the current decoder's subtitle capability.
// Streams and Text are immutable after publication to the browser.
type VideoTracks struct {
	TrackOptions
	SourceID        string
	Streams         []media.MediaStream
	ClientSubtitles bool
	LivePicture     bool // Decoder supports changing fit without replacing the stream.
}

// Stream looks up a server index without assuming indexes are contiguous.
func (t VideoTracks) Stream(kind string, index int) (media.MediaStream, bool) {
	for _, s := range t.Streams {
		if s.Type == kind && s.Index == index {
			return s, true
		}
	}
	return media.MediaStream{}, false
}

func videoTracks(item media.Item, choices trackPreparation) (VideoTracks, error) {
	t := VideoTracks{SourceID: item.ID, Streams: item.MediaStreams, ClientSubtitles: choices.clientSubtitles, TrackOptions: TrackOptions{Selection: media.TrackSelection{AudioIndex: -1, SubtitleIndex: -1}}}
	if len(item.MediaSources) > 0 {
		t.SourceID = item.MediaSources[0].ID
		if len(item.MediaSources[0].MediaStreams) > 0 {
			t.Streams = item.MediaSources[0].MediaStreams
		}
	}
	if len(t.Streams) > 256 {
		return t, errors.New("too many media streams")
	}
	if choices.saved != nil {
		t.TrackOptions = choices.saved.restore(t)
	} else if choices.explicit != nil {
		t.TrackOptions = *choices.explicit
	}
	t.LivePicture = choices.livePicture
	if t.Picture != PictureOriginal && t.Picture != PictureZoom43 {
		return t, errors.New("unsupported picture mode")
	}
	if t.Selection.AudioIndex >= 0 {
		if _, ok := t.Stream("Audio", t.Selection.AudioIndex); !ok {
			return t, errors.New("audio track is no longer available")
		}
	}
	if t.Selection.SubtitleIndex >= 0 {
		if _, ok := t.Stream("Subtitle", t.Selection.SubtitleIndex); !ok {
			return t, errors.New("subtitle track is no longer available")
		}
	}
	return t, nil
}

// SubtitleResult replaces timed text only after a successful, current download.
// An error leaves the preceding subtitle and stream selection intact.
type SubtitleResult struct {
	Request int
	Index   int
	Text    *subtitles.Track
	Err     error
	serial  int
}

// subtitleLoader owns cancellable extraction requests for one running decoder.
// Only the playback loop mutates its generation and cancel function.
type subtitleLoader struct {
	cancel  context.CancelFunc
	serial  int
	results chan SubtitleResult
}

func (l *subtitleLoader) stop() {
	if l.cancel != nil {
		l.cancel()
	}
}

// subtitleSource exports a selected text track without exposing other playback operations.
type subtitleSource interface {
	Subtitle(context.Context, string, string, int) ([]byte, error)
}

func (l *subtitleLoader) start(ctx context.Context, c subtitleSource, item, source string, index int, request int) {
	l.stop()
	l.serial++
	work, cancel := context.WithCancel(ctx)
	l.cancel = cancel
	serial := l.serial
	go func() {
		result := SubtitleResult{Index: index, serial: serial, Request: request}
		if index >= 0 {
			data, err := c.Subtitle(work, item, source, index)
			if err == nil {
				result.Text, err = subtitles.Parse(data)
			}
			if err != nil {
				result.Err = errors.New("could not load subtitles")
			}
		}
		select {
		case l.results <- result:
		case <-work.Done():
		}
	}()
}
