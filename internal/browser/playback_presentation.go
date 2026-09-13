package browser

import (
	"time"

	"misterfin-go/internal/jellyfin"
)

// PlaybackPresentation is a value snapshot. It contains no decoder handles,
// mutable pointers, navigation state, or output-specific information.
type PlaybackPresentation struct {
	// Active remains true during a seek handoff. Audio identifies the media type,
	// independent of whether a music queue is waiting for its next track.
	Active bool
	Audio  bool

	Title           string
	PositionTicks   int64
	DurationTicks   int64
	Paused          bool
	ControlsVisible bool
	Seekable        bool

	DestinationTicks int64 // Absolute position, valid only when HasDestination is true.
	HasDestination   bool
	ShowDestination  bool // Two or more presses, while waiting for the seek deadline.
	WaitLabel        string
	Notice           string
}

func (s *playbackState) presentation(item *jellyfin.Item, now time.Time) PlaybackPresentation {
	p := PlaybackPresentation{
		PositionTicks:   s.PositionTicks,
		Paused:          s.Paused,
		ControlsVisible: s.ControlsVisible(now) && item != nil,
		WaitLabel:       s.videoWaitLabel(now),
	}
	if item != nil {
		p.Title = item.Name
		p.DurationTicks = item.RunTimeTicks
		p.Seekable = !jellyfin.IsLive(*item)
	}
	if s.SeekTarget != nil {
		p.HasDestination = true
		p.DestinationTicks = *s.SeekTarget
		p.ShowDestination = s.SeekPresses >= 2 && !s.SeekInFlight
	}
	return p
}
