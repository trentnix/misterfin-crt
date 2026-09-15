package browser

import (
	"testing"
	"time"

	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/rendering"
)

func TestSceneCopiesScalarState(t *testing.T) {
	m, art := New(), rendering.Artwork{}
	m.Notice = "original"
	playback := rendering.PlaybackPresentation{PositionTicks: 100}
	scene := sceneFromModel(m, playback, rendering.SetupPresentation{Kind: rendering.SetupConnecting}, selectionData{artwork: art}, "", time.Unix(100, 0))
	m.Notice = "changed"
	playback.PositionTicks = 200
	m.Current().Selected = 1
	if scene.Notice != "original" || scene.Playback.PositionTicks != 100 || scene.Content.Selected != 0 {
		t.Fatal("scene scalars changed with model")
	}
}

func TestScenePreservesBorrowedItemsAndActions(t *testing.T) {
	m := New()
	m.Current().Location = jellyfin.Location{Kind: "items", ParentID: "library", Collection: "music"}
	m.Current().Page.Items = []jellyfin.Item{{Name: "Artist", Type: "MusicArtist"}}
	now := time.Unix(100, 0)
	snapshot := func() rendering.Scene {
		return sceneFromModel(m, rendering.PlaybackPresentation{}, rendering.SetupPresentation{}, selectionData{}, "", now)
	}
	scene := snapshot()
	if !scene.Content.CanShuffle || scene.Content.CanResume || scene.Content.Item() != &m.Current().Page.Items[0] {
		t.Fatal("artist list lost shuffle or copied its items")
	}
	artistIdentity := scene.Content.Identity
	m.Current().Location.ParentID = "another library"
	if snapshot().Content.Identity == artistIdentity {
		t.Fatal("different lists share scroll animation")
	}
	for _, kind := range []string{"Movie", "Episode", "TvChannel", "Audio", "Photo"} {
		item := &jellyfin.Item{Type: kind}
		item.UserData.PlaybackPositionTicks = 900000000
		m.Current().Detail = item
		scene = snapshot()
		if scene.Content.Detail != item || scene.Content.CanShuffle || scene.Content.CanResume != (kind == "Movie" || kind == "Episode") {
			t.Fatalf("incorrect detail actions for %s", kind)
		}
		item.UserData.Played = true
		if snapshot().Content.CanResume {
			t.Fatal("watched item advertised restart")
		}
	}
	m.Current().Detail = nil
	m.Current().Location.Kind = "continue"
	if !snapshot().Content.Continue {
		t.Fatal("Continue list lost its labels")
	}
	if allocations := testing.AllocsPerRun(100, func() { scene = snapshot() }); allocations != 0 {
		t.Fatalf("scene projection allocated %v times per frame", allocations)
	}
}
