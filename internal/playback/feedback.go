package playback

import "misterfin-crt/internal/player"

// publishFeedback receives normalized observations from a decoder's output
// writer. It never waits for the playback loop. Measurements can be dropped,
// while captions and picture acknowledgments retain the newest queued state.
func (p *playerProcess) publishFeedback(value player.Feedback) {
	switch value.Kind {
	case player.FeedbackPosition:
		select {
		case p.positions <- value.Position:
		default:
		}
	case player.FeedbackLevels:
		select {
		case p.levels <- value.Levels:
		default:
		}
	case player.FeedbackBuffering:
		select {
		case p.buffering <- value.Buffering:
		default:
		}
	case player.FeedbackVideoStarted:
		select {
		case p.videoStarted <- struct{}{}:
		default:
		}
	case player.FeedbackPicture:
		publishLatest(p.pictures, value.Picture)
	case player.FeedbackCaption:
		publishLatest(p.captions, value.Caption)
	}
}

// publishLatest replaces the oldest queued state when full. The decoder writer
// serializes producers. Playback is the only other channel consumer.
func publishLatest[T any](ch chan T, value T) {
	select {
	case ch <- value:
		return
	default:
	}
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- value:
	default:
	}
}
