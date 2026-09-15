package browser

import (
	"math/rand/v2"
	"slices"
	"time"

	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/remote"
)

// remotePlayback owns queue entries, their metadata, and decoder handoff state.
// A replacement waits for decoder cleanup before starting the selected entry.
type remotePlayback struct {
	paused bool
	queue  remote.Queue
	items  map[string]jellyfin.Item
	// active gives this queue ownership of track advancement. During switching,
	// Current already identifies the next item while the old decoder stops.
	active, switching bool
	// returnDepth restores the browser stack when the queue ends.
	returnDepth int
	// start is consumed once by the next decoder start. Nil starts at zero.
	start *int64
}

// replace installs one catalog snapshot without changing playback or navigation.
func (q *remotePlayback) replace(items []jellyfin.Item, index int) {
	q.items = nil
	q.queue.Replace(q.remember(items), index)
}

// remember indexes metadata once while preserving duplicate queue occurrences.
func (q *remotePlayback) remember(items []jellyfin.Item) []string {
	if q.items == nil {
		q.items = make(map[string]jellyfin.Item, len(items))
	}
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
		q.items[item.ID] = item
	}
	return ids
}

func (s *browserSession) applyRemoteItems(cmd remote.Command, items []jellyfin.Item) {
	q := &s.remotePlayback
	appendQueue := cmd.PlayMode == remote.PlayNext || cmd.PlayMode == remote.PlayLast
	if appendQueue && !q.active {
		s.adoptLocalQueue()
	}
	if q.active && appendQueue && q.queue.Len()+len(items) > 10000 {
		s.message = MessagePresentation{Header: "Remote playback", Text: "The queue limit is 10000 items.", Until: time.Now().Add(8 * time.Second)}
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
	s.switchRemoteItem()
}

// adoptLocalQueue exposes complete loaded album queues without a new request.
// A partial library keeps its existing paged navigation until an explicit queue
// is requested. Single recorded videos can repeat without changing their view.
func (s *browserSession) adoptLocalQueue() {
	if !s.controller.running {
		return
	}
	item := s.controller.item
	q := &s.remotePlayback
	items := []jellyfin.Item{item}
	index := 0
	wasShuffle := s.shuffle.library != "" && len(s.shuffle.items) > 0
	if wasShuffle {
		items = s.shuffle.items
		index = s.shuffle.position
	} else if item.Type == "Audio" {
		if parent, ok := s.model.Parent(); ok && parent.Start == 0 && !parent.More() {
			tracks := audioItems(parent.Page.Items)
			if selected := slices.IndexFunc(tracks, func(track jellyfin.Item) bool { return track.ID == item.ID }); selected >= 0 {
				items, index = tracks, selected
			}
		}
	}
	q.replace(items, index)
	if wasShuffle {
		q.queue.SetShuffle(true)
		s.shuffle = shuffleQueue{}
	}
	q.active = true
	q.returnDepth = len(s.model.Stack)
	if item.Type == "Audio" || jellyfin.IsLive(item) {
		q.returnDepth = max(1, q.returnDepth-1)
	}
	s.publishRemoteQueue()
}

func (s *browserSession) switchRemoteItem() {
	q := &s.remotePlayback
	q.switching = true
	if s.controller.running {
		s.controller.stopByUser()
		if s.controller.running {
			return
		}
	}
	s.startRemoteItem()
}

func (s *browserSession) startRemoteItem() {
	q := &s.remotePlayback
	item, ok := q.items[q.queue.Current().ID]
	if !ok {
		s.endRemoteQueue()
		return
	}
	q.switching = false
	// Keep a visible library row in step with track changes. Remote queues can
	// contain items from other libraries, so only update an existing matching row.
	if len(s.model.Stack) > 1 {
		parent := &s.model.Stack[len(s.model.Stack)-2]
		for i, entry := range parent.Page.Items {
			if entry.ID == item.ID {
				parent.Selected = i
				parent.Target = parent.Start + i
				parent.centerSelection(s.model.Rows)
				break
			}
		}
	}
	s.model.Current().Detail = &item
	s.model.Current().Title = item.Name
	s.model.EndMusicQueue()
	s.selection.key = ""
	s.loadSelection()
	start := q.start
	q.start = nil
	if start == nil {
		zero := int64(0)
		start = &zero
	}
	s.publishRemoteQueue()
	s.startPlayback(start, q.paused)
}

func (s *browserSession) moveRemoteQueue(direction int, natural bool) bool {
	q := &s.remotePlayback
	if !q.active || !q.queue.Move(direction, natural) {
		return false
	}
	q.start = nil
	q.paused = false
	s.switchRemoteItem()
	return true
}

// remoteEnded consumes real item completion, never a seek replacement event.
func (s *browserSession) remoteEnded(event PlaybackEvent) bool {
	q := &s.remotePlayback
	if !q.active {
		return false
	}
	if q.switching {
		s.startRemoteItem()
		return true
	}
	if !s.controller.stoppedByUser && event.Err == nil && s.moveRemoteQueue(1, true) {
		return true
	}
	s.endRemoteQueue()
	if event.Err != nil {
		s.message = MessagePresentation{Header: "Playback", Text: "Playback ended with an error.", Until: time.Now().Add(8 * time.Second)}
	}
	return true
}

func (s *browserSession) endRemoteQueue() {
	q := &s.remotePlayback
	depth := q.returnDepth
	q.active = false
	q.switching = false
	q.queue.Replace(nil, 0)
	q.items = nil
	s.model.EndMusicQueue()
	s.shuffle = shuffleQueue{}
	if depth > 0 && depth < len(s.model.Stack) {
		s.model.Stack = s.model.Stack[:depth]
	}
	s.selection.key = ""
	s.loadSelection()
	s.publishRemoteQueue()
}
