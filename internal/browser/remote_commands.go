package browser

import (
	"math/rand/v2"
	"time"

	"mistervision/internal/media"
	"mistervision/internal/remote"
	"mistervision/internal/rendering"
)

// handleRemote operates on media state directly. It never synthesizes a select
// button, so About, track pickers, and hidden overlays cannot consume commands.
func (s *browserSession) handleRemote(cmd remote.Command) bool {
	if s.about.Updating || !s.update.exitAt.IsZero() {
		return false
	}
	now := time.Now()
	switch cmd.Kind {
	case remote.Message:
		s.message = browserMessage{MessagePresentation: rendering.MessagePresentation{Header: cmd.Header, Text: cmd.Text, Until: now.Add(8 * time.Second)}}
	case remote.Play:
		s.requestRemotePlay(cmd)
	case remote.Stop:
		s.remoteRequests.cancelAll()
		s.playbackQueue.switching = false
		if s.media.pending {
			s.media.cancel()
			s.media.generation++
			s.media.pending = false
			s.media.queued = nil
		}
		if s.controller.running {
			s.controller.stopByUser()
		} else if s.playbackQueue.active {
			s.endQueue()
		} else if s.playlistPlayback() {
			s.model.ReturnToParent()
			s.loadSelection()
		}
	case remote.Pause, remote.Resume, remote.TogglePause:
		paused := cmd.Kind == remote.Pause
		if cmd.Kind == remote.TogglePause {
			paused = !s.wantsPause()
		}
		return s.setPaused(paused)
	case remote.Seek:
		if cmd.Position != nil {
			s.controller.SeekTo(*cmd.Position, now)
		}
	case remote.Next, remote.Previous:
		direction := 1
		if cmd.Kind == remote.Previous {
			direction = -1
		}
		if s.playbackQueue.active {
			s.moveQueue(direction, false)
		} else if s.model.MusicQueueActive() || s.playlistPlayback() {
			s.media.nextTrack = direction
			s.navigateMedia(direction)
		}
	case remote.Repeat, remote.Shuffle:
		if !s.playbackQueue.active {
			s.adoptLocalQueue()
		}
		if !s.playbackQueue.active {
			return false
		}
		if cmd.Kind == remote.Repeat {
			s.playbackQueue.queue.SetRepeat(cmd.Repeat)
		} else {
			s.playbackQueue.queue.SetShuffle(cmd.Shuffled)
		}
		s.publishRemoteQueue()
	default:
		return false
	}
	return true
}

// applyRemoteItems installs resolved remote requests in the shared playback queue.
func (s *browserSession) applyRemoteItems(cmd remote.Command, items []media.Item) {
	q := &s.playbackQueue
	appendQueue := cmd.PlayMode == remote.PlayNext || cmd.PlayMode == remote.PlayLast
	if appendQueue && !q.active {
		s.adoptLocalQueue()
	}
	if q.active && appendQueue && q.queue.Len()+len(items) > 10000 {
		s.message = browserMessage{MessagePresentation: rendering.MessagePresentation{Header: titleRemotePlayback, Text: messageQueueLimit, Until: time.Now().Add(8 * time.Second)}}
		return
	}
	if cmd.PlayMode == remote.PlayShuffle {
		rand.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
	}
	if q.active && appendQueue {
		q.queue.Append(q.remember(items), cmd.PlayMode == remote.PlayNext)
		s.publishRemoteQueue()
		return
	}
	keepPlaying := q.active && !q.switching && s.controller.running && cmd.PlayMode == remote.PlayNow && cmd.Position == nil && len(items) > 1 && items[max(0, min(cmd.StartIndex, len(items)-1))].ID == s.controller.item.ID
	if !q.active {
		q.returnDepth = len(s.model.Stack)
		q.active = true
		s.model.Stack = append(s.model.Stack, View{})
	}
	q.replace(items, cmd.StartIndex)
	q.paused = false
	if cmd.PlayMode == remote.PlayShuffle {
		q.queue.SetShuffle(true)
	}
	if keepPlaying {
		s.publishRemoteQueue()
		return
	}
	q.start = cmd.Position
	s.about.Visible = false
	s.model.ExitConfirm = false
	s.requests.cancel()
	s.model.Generation++
	s.media.cancel()
	s.media.generation++
	s.media.pending = false
	s.media.queued = nil
	s.shuffle = shuffleQueue{}
	s.switchQueueItem()
}
