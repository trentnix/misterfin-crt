package browser

import (
	"errors"
	"testing"

	"mistervision/internal/input/control"
	"mistervision/internal/media"
	"mistervision/internal/remote"
)

func TestShuffleCancellationPreservesArtists(t *testing.T) {
	for _, playing := range []bool{false, true} {
		s := testSession(t)
		s.controller.running = false
		artist := View{Location: media.Location{Collection: "music", ParentID: "library"}, Page: media.Page{Items: []media.Item{{ID: "artist", Type: "MusicArtist"}}}}
		s.model.Stack = append(s.model.Stack, artist)
		s.shuffle = shuffleQueue{library: "library"}
		s.media.pending = true
		s.media.generation = 2
		if playing {
			s.model.Stack = append(s.model.Stack, View{Detail: &media.Item{ID: "track", Type: "Audio"}})
			s.model.StartMusicQueue()
		}
		s.handleKey(control.Back)
		if len(s.model.Stack) != 2 || s.model.Current().Item().ID != "artist" || s.model.MusicQueueActive() || s.shuffle.library != "" {
			t.Fatalf("cancel playing=%v lost artists: %+v", playing, s.model)
		}
		if s.handleShuffle(shuffleResult{generation: 2, page: media.Page{Items: []media.Item{{ID: "stale", Type: "Audio"}}}}) {
			t.Fatal("accepted canceled shuffle")
		}
	}
}

func TestShuffleRefillAndStopRestoreArtistSelection(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	s.model.Stack = append(s.model.Stack, View{Location: media.Location{Collection: "music", ParentID: "library"}, Selected: 1, Page: media.Page{Items: []media.Item{{ID: "first", Type: "MusicArtist"}, {ID: "selected", Type: "MusicArtist"}}}})
	s.shuffle = shuffleQueue{library: "library", position: -1}
	s.media.generation = 1
	one, two := media.Item{ID: "one", Type: "Audio"}, media.Item{ID: "two", Type: "Audio"}
	s.handleShuffle(shuffleResult{generation: 1, page: media.Page{Items: []media.Item{one, two}}})
	if !s.model.MusicQueueActive() || s.model.Current().Detail.ID != "one" {
		t.Fatal("shuffle did not start")
	}
	s.navigateShuffle(1)
	s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: s.controller.active.id})
	if s.model.Current().Detail.ID != "two" {
		t.Fatal("next did not advance")
	}
	s.navigateShuffle(-1)
	s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: s.controller.active.id})
	if s.model.Current().Detail.ID != "one" {
		t.Fatal("previous did not follow history")
	}
	s.handleMusicKey("back")
	s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: s.controller.active.id})
	if len(s.model.Stack) != 2 || s.model.Current().Item().ID != "selected" || s.shuffle.library != "" {
		t.Fatal("stop did not restore selected artist")
	}
}

func TestShuffleFailureLeavesRetryNotice(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	s.media.generation = 3
	s.shuffle.library = "library"
	s.handleShuffle(shuffleResult{generation: 3, err: errors.New("offline")})
	if s.model.Notice == "" || s.shuffle.library != "" || s.media.pending {
		t.Fatal("shuffle failure lost retry state")
	}
}

func TestShuffleTransitionPreservesExplicitPause(t *testing.T) {
	s := setupMusicSession(t)
	parent, _ := s.model.Parent()
	s.shuffle = shuffleQueue{library: "music", items: parent.Page.Items, position: 0}
	old := s.controller.active.id
	s.handleRemote(remote.Command{Kind: remote.Next})
	s.handleRemote(remote.Command{Kind: remote.Pause})
	s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: old})
	if s.controller.item.ID != "second" || !s.controller.wantsPause() {
		t.Fatal("shuffle transition lost the requested pause")
	}
}
