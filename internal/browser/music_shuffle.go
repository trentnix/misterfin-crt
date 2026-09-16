package browser

import (
	"context"

	"misterfin-crt/internal/media"
)

// shuffleQueue leaves the artist view intact. Its bounded history supports
// previous/next without changing the library's listing or pagination state.
type shuffleQueue struct {
	library  string
	items    []media.Item
	position int
}

func canShuffle(v View) bool {
	return v.Detail == nil && v.Location.Collection == "music" && v.Item() != nil && v.Item().Type == "MusicArtist"
}

func (s *browserSession) startShuffle() {
	s.requests.cancel()
	s.model.Generation++
	s.model.Current().Loading = false
	s.model.Current().fetching = false
	s.shuffle = shuffleQueue{library: s.model.Current().Location.ParentID, position: -1}
	s.fetchShuffle()
}

func (s *browserSession) fetchShuffle() {
	if s.media.pending {
		return
	}
	s.media.cancel()
	s.media.generation++
	generation, library, client := s.media.generation, s.shuffle.library, s.client
	ctx, cancel := context.WithCancel(s.ctx)
	s.media.cancel, s.media.pending = cancel, true
	s.model.Notice = "Loading shuffle..."
	go func() {
		items, err := client.RandomTracks(ctx, library)
		s.send(ctx, shuffleResult{generation: generation, page: media.Page{Items: items}, err: err})
	}()
}

func (s *browserSession) handleShuffle(r shuffleResult) bool {
	if r.generation != s.media.generation {
		return false
	}
	s.media.pending = false
	s.model.Notice = ""
	if r.err != nil || len(r.page.Items) == 0 {
		if !s.controller.running {
			if s.model.MusicQueueActive() {
				s.model.ReturnToParent()
			}
			s.shuffle = shuffleQueue{}
			s.loadSelection()
		}
		s.model.Notice = "Could not load shuffle. Select shuffle to retry."
		return true
	}
	// Keep one previous batch. Avoid an immediate repeat at a batch boundary.
	items := r.page.Items
	if n := len(s.shuffle.items); n > 0 && len(items) > 1 && items[0].ID == s.shuffle.items[n-1].ID {
		items[0], items[1] = items[1], items[0]
	}
	if len(s.shuffle.items) > 64 {
		s.shuffle.items = append([]media.Item(nil), s.shuffle.items[len(s.shuffle.items)-64:]...)
	}
	s.shuffle.position = len(s.shuffle.items)
	s.shuffle.items = append(s.shuffle.items, items...)
	s.selectShuffleTrack()
	return true
}

func (s *browserSession) navigateShuffle(direction int) {
	if s.media.pending {
		return
	}
	next := s.shuffle.position + direction
	if next < 0 {
		return
	}
	if next >= len(s.shuffle.items) {
		s.fetchShuffle()
		return
	}
	s.shuffle.position = next
	s.selectShuffleTrack()
}

func (s *browserSession) selectShuffleTrack() {
	item := s.shuffle.items[s.shuffle.position]
	if s.controller.running {
		parent, _ := s.model.Parent()
		s.media.queued = &mediaSelection{parent: parent, item: item}
		s.controller.StopForTrackChange()
		return
	}
	if !s.model.MusicQueueActive() {
		s.model.Stack = append(s.model.Stack, View{Title: item.Name, Detail: &item})
	} else {
		s.model.Current().Detail = &item
		s.model.Current().Title = item.Name
	}
	s.selection.key = ""
	s.loadSelection()
	s.startPlayback(nil, false)
}
