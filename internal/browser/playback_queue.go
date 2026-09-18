package browser

import (
	"mistervision/internal/media"
	"mistervision/internal/remote"
)

// playbackQueue owns local and remote queue entries, metadata, and decoder handoff.
// A replacement waits for decoder cleanup before starting the selected entry.
type playbackQueue struct {
	paused bool
	queue  remote.Queue
	items  map[string]media.Item
	// localRows maps queue occurrence keys to browser rows when adopting a local
	// queue. Repeated media IDs must not move the highlight to their first row.
	localRows map[string]int
	// active gives this queue ownership of track advancement. During switching,
	// Current already identifies the next item while the old decoder stops.
	active, switching bool
	// returnDepth restores the browser stack when the queue ends.
	returnDepth int
	// start is consumed once by the next decoder start. Nil starts at zero.
	start *int64
}

// replace installs one catalog snapshot without changing playback or navigation.
func (q *playbackQueue) replace(items []media.Item, index int) {
	q.items = nil
	q.localRows = nil
	q.queue.Replace(q.remember(items), index)
}

// remember indexes metadata once while preserving duplicate queue occurrences.
func (q *playbackQueue) remember(items []media.Item) []string {
	if q.items == nil {
		q.items = make(map[string]media.Item, len(items))
	}
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
		q.items[item.ID] = item
	}
	return ids
}

// adoptLocalQueue exposes complete loaded album queues without a new request.
// A partial library keeps its existing paged navigation until an explicit queue
// is requested. Single recorded videos can repeat without changing their view.
func (s *browserSession) adoptLocalQueue() {
	if !s.controller.running {
		return
	}
	item := s.controller.item
	q := &s.playbackQueue
	items := []media.Item{item}
	index := 0
	var rows []int
	if parent, ok := s.model.Parent(); ok && parent.Item() != nil && parent.Item().ID == item.ID {
		rows = []int{parent.Start + parent.Selected}
	}
	wasShuffle := s.shuffle.library != "" && len(s.shuffle.items) > 0
	if wasShuffle {
		items = s.shuffle.items
		index = s.shuffle.position
		rows = nil
	} else if item.Type == "Audio" {
		if parent, ok := s.model.Parent(); ok && parent.Start == 0 && !parent.More() {
			tracks := audioItems(parent.Page.Items)
			if selected := parent.Item(); selected != nil && selected.ID == item.ID && selected.Type == "Audio" {
				items = tracks
				rows = nil
				for row, track := range parent.Page.Items {
					if track.Type == "Audio" {
						if row == parent.Selected {
							index = len(rows)
						}
						rows = append(rows, row)
					}
				}
			}
		}
	}
	q.replace(items, index)
	if len(rows) == len(items) {
		q.localRows = make(map[string]int, len(rows))
		for i, entry := range q.queue.Snapshot().Entries {
			q.localRows[entry.Key] = rows[i]
		}
	}
	if wasShuffle {
		q.queue.SetShuffle(true)
		s.shuffle = shuffleQueue{}
	}
	q.active = true
	q.returnDepth = len(s.model.Stack)
	if item.Type == "Audio" || media.IsLive(item) {
		q.returnDepth = max(1, q.returnDepth-1)
	}
	s.publishRemoteQueue()
}

// switchQueueItem stops the old decoder before handing off to the selected entry.
func (s *browserSession) switchQueueItem() {
	q := &s.playbackQueue
	q.switching = true
	if s.controller.running {
		s.controller.stopByUser()
		if s.controller.running {
			return
		}
	}
	s.startQueueItem()
}

// startQueueItem updates the browser selection and starts the chosen queue entry.
func (s *browserSession) startQueueItem() {
	q := &s.playbackQueue
	item, ok := q.items[q.queue.Current().ID]
	if !ok {
		s.endQueue()
		return
	}
	q.switching = false
	// Keep a visible library row in step with track changes. Remote queues can
	// contain items from other libraries, so only update an existing matching row.
	if len(s.model.Stack) > 1 {
		parent := &s.model.Stack[len(s.model.Stack)-2]
		row, local := q.localRows[q.queue.Current().Key]
		for i, entry := range parent.Page.Items {
			if entry.ID == item.ID && (!local || parent.Start+i == row) {
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

// moveQueue advances according to repeat and shuffle settings. Natural marks EOF.
func (s *browserSession) moveQueue(direction int, natural bool) bool {
	q := &s.playbackQueue
	if !q.active || !q.queue.Move(direction, natural) {
		return false
	}
	q.start = nil
	q.paused = false
	s.switchQueueItem()
	return true
}

// queueEnded consumes real item completion, never a seek replacement event.
func (s *browserSession) queueEnded(event PlaybackEvent) bool {
	q := &s.playbackQueue
	if !q.active {
		return false
	}
	if q.switching {
		s.startQueueItem()
		return true
	}
	if !s.controller.stoppedByUser && event.Err == nil && s.moveQueue(1, true) {
		return true
	}
	s.endQueue()
	return true
}

// endQueue releases track advancement and restores the previous browser view.
func (s *browserSession) endQueue() {
	q := &s.playbackQueue
	depth := q.returnDepth
	q.active = false
	q.switching = false
	q.queue.Replace(nil, 0)
	q.items = nil
	q.localRows = nil
	s.model.EndMusicQueue()
	s.shuffle = shuffleQueue{}
	if depth > 0 && depth < len(s.model.Stack) {
		s.model.Stack = s.model.Stack[:depth]
	}
	s.selection.key = ""
	s.loadSelection()
	s.publishRemoteQueue()
}
