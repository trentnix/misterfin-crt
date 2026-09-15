package browser

import (
	"time"

	"misterfin-crt/internal/remote"
)

// handleRemote operates on media state directly. It never synthesizes a select
// button, so About, track pickers, and hidden overlays cannot consume commands.
func (s *browserSession) handleRemote(cmd remote.Command) bool {
	now := time.Now()
	switch cmd.Kind {
	case remote.Message:
		s.message = MessagePresentation{Header: cmd.Header, Text: cmd.Text, Until: now.Add(8 * time.Second)}
	case remote.Play:
		s.requestRemotePlay(cmd)
	case remote.Stop:
		s.remoteRequests.cancelAll()
		s.remotePlayback.switching = false
		if s.media.pending {
			s.media.cancel()
			s.media.generation++
			s.media.pending = false
			s.media.queued = nil
		}
		if s.controller.running {
			s.controller.stopByUser()
		} else if s.remotePlayback.active {
			s.endRemoteQueue()
		}
	case remote.Pause, remote.Resume, remote.TogglePause:
		if !s.controller.running {
			return false
		}
		paused := cmd.Kind == remote.Pause
		if cmd.Kind == remote.TogglePause {
			paused = !s.controller.wantsPause()
		}
		if s.remotePlayback.switching {
			s.remotePlayback.paused = paused
		}
		s.controller.SetPaused(paused)
	case remote.Seek:
		if cmd.Position != nil {
			s.controller.SeekTo(*cmd.Position, now)
		}
	case remote.Next, remote.Previous:
		direction := 1
		if cmd.Kind == remote.Previous {
			direction = -1
		}
		if s.remotePlayback.active {
			s.moveRemoteQueue(direction, false)
		} else if s.model.MusicQueueActive() {
			s.media.nextTrack = direction
			s.navigateMedia(direction)
		}
	case remote.Repeat, remote.Shuffle:
		if !s.remotePlayback.active {
			s.adoptLocalQueue()
		}
		if !s.remotePlayback.active {
			return false
		}
		if cmd.Kind == remote.Repeat {
			s.remotePlayback.queue.SetRepeat(cmd.Repeat)
		} else {
			s.remotePlayback.queue.SetShuffle(cmd.Shuffled)
		}
		s.publishRemoteQueue()
	default:
		return false
	}
	return true
}
