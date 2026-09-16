package media

import (
	"crypto/rand"
	"encoding/hex"
)

// NewPlaySessionID returns a cryptographically random identifier for one
// playback session, or an error if secure randomness is unavailable.
func NewPlaySessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "misterfin-crt-" + hex.EncodeToString(b[:]), nil
}

// PlayState is a snapshot of application playback in 100-nanosecond ticks.
// Adapters translate these facts into their own reporting payloads. Optional
// flags distinguish unavailable information from a known false value.
type PlayState struct {
	ItemID, PlaySessionID, MediaSourceID  string
	AudioStreamIndex, SubtitleStreamIndex *int
	CanSeek, Failed                       *bool
	PositionTicks                         int64
	IsPaused, Audio                       bool
}

// IsLive recognizes the supported channel types.
func IsLive(item Item) bool {
	return item.Type == "TvChannel" || item.Type == "LiveTvChannel"
}
