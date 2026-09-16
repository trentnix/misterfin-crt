package media

import (
	"strconv"
	"strings"
)

// TrackSelection uses server stream identifiers, not positions in the stream list.
// AudioIndex -1 lets the server choose. SubtitleIndex -1 disables subtitles.
type TrackSelection struct{ AudioIndex, SubtitleIndex int }

// TextSubtitle reports whether the codec represents a text subtitle.
// Unknown codecs use server burn-in, following the C client's fallback.
func (s MediaStream) TextSubtitle() bool {
	if s.Type != "Subtitle" {
		return false
	}
	switch strings.ToLower(s.Codec) {
	case "subrip", "srt", "ass", "ssa", "vtt", "webvtt", "mov_text", "microdvd", "sami", "smi", "ttml", "stl":
		return true
	}
	return false
}

// ClientSubtitle reports whether this stream can use the shared timed-text
// overlay. Providers can require server rendering even for a text codec.
func (s MediaStream) ClientSubtitle() bool {
	return s.TextSubtitle() && !s.RequiresBurnIn
}

// Label prefers the server's descriptive title, including language and codec.
func (s MediaStream) Label() string {
	label := s.DisplayTitle
	if label != "" && s.Title != "" && !strings.Contains(strings.ToLower(label), strings.ToLower(s.Title)) {
		label += " - " + s.Title
	}
	if label == "" {
		label = s.Title
	}
	if label == "" {
		label = strings.TrimSpace(s.Language + " " + strings.ToUpper(s.Codec))
	}
	if label == "" {
		label = s.Type + " " + strconv.Itoa(s.Index)
	}
	if s.IsForced && !strings.Contains(strings.ToLower(label), "forced") {
		label += " (forced)"
	}
	return label
}
