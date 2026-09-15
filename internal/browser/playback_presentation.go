package browser

import (
	"time"

	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/rendering"
)

// presentation copies the controller values needed to draw controls and seek
// feedback. Decoder resources and mutable control state never enter the snapshot.
func (s *playbackState) presentation(item *jellyfin.Item, now time.Time) rendering.PlaybackPresentation {
	p := rendering.PlaybackPresentation{
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
