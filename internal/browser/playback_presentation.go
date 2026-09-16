package browser

import (
	"time"

	"misterfin-crt/internal/media"
	"misterfin-crt/internal/rendering"
)

// presentation copies the controller values needed to draw controls and seek
// feedback. Decoder resources and mutable control state never enter the snapshot.
func (m *playbackState) presentation(item *media.Item, now time.Time) rendering.PlaybackPresentation {
	p := rendering.PlaybackPresentation{
		PositionTicks:   m.PositionTicks,
		Paused:          m.Paused,
		ControlsVisible: m.ControlsVisible(now) && item != nil,
		WaitLabel:       m.videoWaitLabel(now),
	}
	if item != nil {
		p.Title = item.Name
		p.DurationTicks = item.RunTimeTicks
		p.Seekable = !media.IsLive(*item)
	}
	if m.SeekTarget != nil {
		p.HasDestination = true
		p.DestinationTicks = *m.SeekTarget
		p.ShowDestination = m.SeekPresses >= 2 && !m.SeekInFlight
	}
	return p
}
