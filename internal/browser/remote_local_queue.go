package browser

import (
	"context"
	"slices"
	"strconv"

	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/remote"
)

// publishLocalQueue makes locally started media visible to remote controllers.
// Complete album pages are reused. Longer lists load in the background without
// delaying playback. Existing whole-library shuffle keeps its rolling batches.
func (s *browserSession) publishLocalQueue() {
	q := &s.remoteRequests
	if s.remote.source == nil || s.remotePlayback.active {
		return
	}
	q.cancelLocal()
	item := s.controller.item
	if s.shuffle.library != "" {
		state := remote.QueueState{Repeat: remote.RepeatNone, Shuffled: true}
		for i, track := range s.shuffle.items {
			key := "shuffle-" + strconv.Itoa(i)
			state.Entries = append(state.Entries, remote.Entry{ID: track.ID, Key: key})
			if i == s.shuffle.position {
				state.Current = key
			}
		}
		s.remote.source.Publish(state)
		return
	}
	s.remote.source.Publish(remote.QueueState{Entries: []remote.Entry{{ID: item.ID, Key: "local"}}, Current: "local", Repeat: remote.RepeatNone})
	parent, hasParent := s.model.Parent()
	if item.Type != "Audio" || !hasParent || (parent.Start == 0 && !parent.More()) {
		s.adoptLocalQueue()
		return
	}
	if parent.Location.Kind == "" {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, remoteRequestTimeout)
	q.localCancel = cancel
	generation, client := q.localGeneration, s.client
	go func() {
		defer cancel()
		items, err := client.AudioQueue(ctx, parent.Location)
		s.send(s.ctx, localQueueResult{generation, item.ID, items, err})
	}()
}

type localQueueResult struct {
	generation int
	itemID     string
	items      []jellyfin.Item
	err        error
}

func (r localQueueResult) apply(s *browserSession) bool {
	q := &s.remotePlayback
	if r.generation != s.remoteRequests.localGeneration || !s.controller.running || q.active || s.controller.item.ID != r.itemID || r.err != nil {
		return false
	}
	index := slices.IndexFunc(r.items, func(item jellyfin.Item) bool { return item.ID == r.itemID })
	if index < 0 {
		return false
	}
	q.replace(r.items, index)
	q.active = true
	q.returnDepth = max(1, len(s.model.Stack)-1)
	s.publishRemoteQueue()
	return false
}

// audioItems preserves the order of playable tracks in an already loaded page.
func audioItems(items []jellyfin.Item) []jellyfin.Item {
	var tracks []jellyfin.Item
	for _, item := range items {
		if item.Type == "Audio" {
			tracks = append(tracks, item)
		}
	}
	return tracks
}
