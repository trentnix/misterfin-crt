package browser

import (
	"context"
	"errors"
	"testing"

	"mistervision/internal/input/control"
	"mistervision/internal/media"
	"mistervision/internal/remote"
)

func TestHomeExcludesReadingLibrariesByCategory(t *testing.T) {
	s := testSession(t)
	s.home.loaded = true
	items := []media.Item{{ID: "movie", Name: "Books", CollectionType: "movies"}, {ID: "music", CollectionType: "music"}}
	for _, category := range []string{"books", "comics", "audiobooks", "AudioBooks"} {
		items = append(items, media.Item{ID: category, Name: "My Library", CollectionType: category})
	}
	page := s.homeLibraries(media.Page{Items: items})
	if len(page.Items) != 2 || *page.TotalRecordCount != 2 || page.Items[0].ID != "movie" {
		t.Fatalf("carousel includes reading formats or filters display names: %+v", page)
	}
}

func TestCollectionAndPlaylistNavigation(t *testing.T) {
	for _, tc := range []struct {
		root bool
		item media.Item
		kind string
	}{
		{true, media.Item{ID: "sets", CollectionType: "boxsets"}, "collections"},
		{true, media.Item{ID: "lists", CollectionType: "playlists"}, "playlists"},
		{false, media.Item{ID: "set", Type: "BoxSet"}, "collection"},
		{false, media.Item{ID: "list", Type: "Playlist"}, "playlist"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			m := New()
			if !tc.root {
				m.Current().Location = media.Location{Kind: "items", Collection: "movies", SeriesID: "old"}
			}
			m.Current().Page.Items = []media.Item{tc.item}
			req := m.Key(control.Open)
			if req == nil || req.Location.Kind != tc.kind || req.Location.ParentID != tc.item.ID {
				t.Fatalf("wrong container request: %+v", req)
			}
			if !tc.root && (req.Location.Collection != "" || req.Location.SeriesID != "") {
				t.Fatal("container inherited library filters")
			}
			m.ReturnToParent()
			if m.Current().Selected != 0 {
				t.Fatal("Back lost selection")
			}
		})
	}
}

// pagedPlaylist verifies offsets rather than item identity. Repeated IDs are
// intentional and must stay distinct when crossing a retained-page boundary.
type pagedPlaylist struct {
	items  []media.Item
	starts []int
}

func (p *pagedPlaylist) List(_ context.Context, loc media.Location, start, limit int) (media.Page, error) {
	if loc.Kind != "playlist" {
		return media.Page{}, errors.New("wrong location")
	}
	p.starts = append(p.starts, start)
	total := len(p.items)
	return media.Page{Items: p.items[min(start, total):min(start+limit, total)], TotalRecordCount: &total}, nil
}

func TestPlaylistNavigationPreservesRepeatedItemsAcrossPages(t *testing.T) {
	p := &pagedPlaylist{items: make([]media.Item, 130)}
	for i := range p.items {
		p.items[i] = media.Item{ID: "repeated", Type: "Audio"}
	}
	p.items[64] = media.Item{ID: "photo", Type: "Photo"}
	p.items[65] = media.Item{ID: "movie", Type: "Movie"}
	total := len(p.items)
	view := View{Location: media.Location{Kind: "playlist", ParentID: "list"}, Page: media.Page{Items: p.items[:64], TotalRecordCount: &total}, Selected: 63}
	next, item, err := adjacentMedia(t.Context(), p, view, "playlist", 1, 6)
	if err != nil || item == nil || item.ID != "movie" || next.Start+next.Selected != 65 {
		t.Fatalf("next: %+v %v", item, err)
	}
	previous, item, err := adjacentMedia(t.Context(), p, next, "playlist", -1, 6)
	if err != nil || item == nil || item.ID != "repeated" || previous.Start+previous.Selected != 63 {
		t.Fatal("previous lost occurrence")
	}
	if len(p.starts) != 1 || p.starts[0] != 64 {
		t.Fatalf("unexpected page requests: %v", p.starts)
	}
}

func playlistSession(t *testing.T, kinds ...string) *browserSession {
	t.Helper()
	s := testSession(t)
	items := make([]media.Item, len(kinds))
	for i, kind := range kinds {
		items[i] = media.Item{ID: "duplicate", Type: kind, Name: kind}
	}
	total := len(items)
	s.model.Current().Location.Kind = "playlist"
	s.model.Current().Page = media.Page{Items: items, TotalRecordCount: &total}
	s.model.Key(control.Open)
	s.startPlayback(nil, false)
	return s
}

func TestPlaylistAdvancesAudioAndVideoAndReturnsToLastRow(t *testing.T) {
	s := playlistSession(t, "Audio", "Audio", "Movie", "Episode")
	for next := 1; next <= 4; next++ {
		s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: s.controller.active.id})
		if !s.media.pending {
			t.Fatal("completion did not request next entry")
		}
		s.handleNeighbor(receiveNeighbor(t, s))
		if next < 4 {
			parent, _ := s.model.Parent()
			if !s.controller.running || parent.Selected != next {
				t.Fatalf("lost playlist position %d", next)
			}
			if s.model.MusicQueueActive() != (next == 1) {
				t.Fatal("music screen retained during video")
			}
		}
	}
	if len(s.model.Stack) != 1 || s.controller.running || s.model.Current().Selected != 3 {
		t.Fatal("end did not restore playlist selection")
	}
}

func TestPlaylistStopErrorAndCancelDoNotAdvance(t *testing.T) {
	for _, mode := range []string{"stop", "error", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s := playlistSession(t, "Movie", "Episode")
			var err error
			switch mode {
			case "stop":
				s.handleKey(control.Back)
			case "error":
				err = errors.New("decoder failed")
			}
			s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: s.controller.active.id, Err: err})
			if mode == "cancel" {
				result := receiveNeighbor(t, s)
				s.handleKey(control.Back)
				s.handleNeighbor(result)
			}
			if s.controller.running || s.media.pending {
				t.Fatal("stopped playlist continued")
			}
			if mode != "error" && len(s.model.Stack) != 1 {
				t.Fatal("did not return to playlist")
			}
		})
	}
}

type playlistRemote struct {
	remote.Source
	published remote.QueueState
}

func (r *playlistRemote) Publish(state remote.QueueState) { r.published = state }

func TestLocalPlaylistStaysPagedWithRemoteControl(t *testing.T) {
	s := playlistSession(t, "Audio", "Audio")
	source := &playlistRemote{}
	s.remote.source = source
	s.publishLocalQueue()
	if s.playbackQueue.active || len(source.published.Entries) != 1 {
		t.Fatal("local playlist was replaced by a remote queue")
	}
	s.handleRemote(remote.Command{Kind: remote.Next})
	s.handleNeighbor(receiveNeighbor(t, s))
	s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: s.controller.active.id})
	parent, _ := s.model.Parent()
	if parent.Selected != 1 {
		t.Fatal("remote Next lost repeated occurrence")
	}
}

func TestAdoptLoadedQueueUsesSelectedOccurrence(t *testing.T) {
	s := playlistSession(t, "Audio", "Audio", "Audio")
	s.model.Stack[0].Selected = 2
	s.adoptLocalQueue()
	state := s.playbackQueue.queue.Snapshot()
	if state.Current != state.Entries[2].Key {
		t.Fatal("adopted first occurrence instead of selected track")
	}
}

func TestAdoptedQueueKeepsRepeatedRowDuringNextAndPrevious(t *testing.T) {
	s := playlistSession(t, "Audio", "Audio", "Audio")
	s.model.Stack[0].Selected = 1
	s.adoptLocalQueue()
	for _, tc := range []struct{ direction, row int }{{1, 2}, {-1, 1}} {
		s.moveQueue(tc.direction, false)
		s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: s.controller.active.id})
		parent, _ := s.model.Parent()
		if parent.Selected != tc.row {
			t.Fatalf("selected row=%d, want %d", parent.Selected, tc.row)
		}
	}
}

func TestPlaylistStopRejectsOutstandingVideoNeighbor(t *testing.T) {
	s := playlistSession(t, "Movie", "Episode")
	s.handleRemote(remote.Command{Kind: remote.Next})
	result := receiveNeighbor(t, s)
	s.handleKey(control.Back)
	s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: s.controller.active.id})
	if s.handleNeighbor(result) || s.controller.running || s.media.pending || len(s.model.Stack) != 1 {
		t.Fatal("late neighbor survived stopping video")
	}
}

func TestRemoteStopCancelsPlaylistHandoff(t *testing.T) {
	s := playlistSession(t, "Movie", "Episode")
	s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: s.controller.active.id})
	result := receiveNeighbor(t, s)
	s.handleRemote(remote.Command{Kind: remote.Stop})
	if s.handleNeighbor(result) || s.controller.running || len(s.model.Stack) != 1 {
		t.Fatal("remote Stop resumed a canceled playlist")
	}
}

func TestCarouselVisibilityOptions(t *testing.T) {
	enabled, disabled := true, false
	for _, tc := range []struct {
		name                           string
		collections, playlists         *bool
		wantCollections, wantPlaylists bool
	}{
		{"defaults", nil, nil, true, true},
		{"enabled", &enabled, &enabled, true, true},
		{"hide collections", &disabled, &enabled, false, true},
		{"hide playlists", &enabled, &disabled, true, false},
		{"hide both", &disabled, &disabled, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testSession(t)
			s.home.loaded = true
			s.home.items = []media.Item{{ID: "resume", Type: "Movie"}}
			s.config.ShowCollections, s.config.ShowPlaylists = tc.collections, tc.playlists
			page := s.homeLibraries(media.Page{Items: []media.Item{
				{ID: "movies", CollectionType: "movies"},
				{ID: "collections", CollectionType: "boxsets"},
				{ID: "playlists", CollectionType: "playlists"},
			}})
			found := make(map[string]bool)
			for _, item := range page.Items {
				found[item.ID] = true
			}
			if !found[continueID] || !found["movies"] || found["collections"] != tc.wantCollections || found["playlists"] != tc.wantPlaylists || *page.TotalRecordCount != len(page.Items) {
				t.Fatalf("wrong carousel: %+v", page)
			}
		})
	}
}
