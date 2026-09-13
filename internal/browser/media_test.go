package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"misterfin-go/internal/jellyfin"
	"misterfin-go/internal/ui"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestResumableVideo(t *testing.T) {
	if resumableVideo(nil) {
		t.Fatal("missing details allowed restart")
	}
	for _, kind := range []string{"Movie", "Episode", "Video", "MusicVideo", "Audio", "Photo", "TvChannel", "LiveTvChannel", "Series"} {
		item := jellyfin.Item{Type: kind}
		if resumableVideo(&item) {
			t.Fatalf("%s without resume position allowed restart", kind)
		}
		item.UserData.PlaybackPositionTicks = 600000000
		want := kind == "Movie" || kind == "Episode" || kind == "Video" || kind == "MusicVideo"
		if resumableVideo(&item) != want {
			t.Fatalf("incorrect restart eligibility for %s", kind)
		}
		item.UserData.Played = true
		if resumableVideo(&item) {
			t.Fatalf("watched %s allowed restart", kind)
		}
	}
}

func TestCleanMusicPauseAndControlTimeout(t *testing.T) {
	m := New()
	state := playbackState{}
	m.Stack = append(m.Stack, View{Detail: &jellyfin.Item{Type: "Audio", Name: "Track", RunTimeTicks: 100000000}})
	m.StartMusicQueue()
	now := time.Unix(100, 0)
	frame := func() []byte {
		return renderScene(ui.New(640, 240), nil, sceneFromModel(m, state.presentation(m.Current().Detail, now), "", selectionData{}, "", now), Animation{})
	}
	playing := frame()
	state.Paused = true
	if !bytes.Equal(playing, frame()) {
		t.Fatal("pause added an overlay")
	}
	if !state.RevealControls(now) {
		t.Fatal("first navigation did not reveal")
	}
	if bytes.Equal(playing, frame()) {
		t.Fatal("controls not visible")
	}
	if state.RevealControls(now.Add(time.Second)) {
		t.Fatal("second navigation consumed")
	}
	now = now.Add(4 * time.Second)
	if state.ControlsVisible(now) {
		t.Fatal("controls did not expire")
	}
	if !bytes.Equal(playing, frame()) {
		t.Fatal("overlay remained after expiry")
	}
	state.RevealControls(now)
	state.HideControls()
	if state.ControlsVisible(now) {
		t.Fatal("play did not hide instructions")
	}
}

func TestMediaNavigationCrossesPagesAndSkipsOnlyForPhotos(t *testing.T) {
	total := 130
	items := make([]jellyfin.Item, total)
	for i := range items {
		items[i] = jellyfin.Item{ID: fmt.Sprint(i), Type: "Photo"}
	}
	items[64].Type = "Video"
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		start, _ := strconv.Atoi(r.URL.Query().Get("StartIndex"))
		json.NewEncoder(w).Encode(jellyfin.Page{Items: items[start:min(start+64, total)], TotalRecordCount: &total})
	}))
	defer server.Close()
	c := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{})
	parent := View{Page: jellyfin.Page{Items: items[:64], TotalRecordCount: &total}, Selected: 63, Location: jellyfin.Location{Kind: "items", ParentID: "folder"}}
	next, item, err := adjacentMedia(context.Background(), c, parent, "Photo", 1, 6)
	if err != nil || item == nil || item.ID != "65" || next.Start+next.Selected != 65 {
		t.Fatal("photo did not cross page", err)
	}
	previous, item, err := adjacentMedia(context.Background(), c, next, "Photo", -1, 6)
	if err != nil || item == nil || item.ID != "63" || previous.Start != 0 {
		t.Fatal("photo did not go back", err)
	}
	items[63].Type = "Audio"
	_, item, err = adjacentMedia(context.Background(), c, parent, "Audio", 1, 6)
	if err != nil || item != nil {
		t.Fatal("music queue crossed a non-audio item")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := requests
	if _, _, err = adjacentMedia(ctx, c, parent, "Photo", 1, 6); err == nil || requests != before {
		t.Fatal("canceled navigation fetched a page")
	}
	for i := range items {
		items[i].Type = "Audio"
	}
	next, item, err = adjacentMedia(context.Background(), c, parent, "Audio", 1, 6)
	if err != nil || item == nil || item.ID != "64" || next.Start+next.Selected != 64 {
		t.Fatal("album queue did not cross page")
	}
}

func TestVideoControlsRestoreCleanFrame(t *testing.T) {
	for _, height := range []int{240, 288} {
		m := New()
		state := playbackState{}
		state.PlayingVideo = true
		state.ProgressSeen, state.BufferingKnown = true, true
		m.Stack = append(m.Stack, View{Detail: &jellyfin.Item{Type: "Movie", Name: "Movie", RunTimeTicks: 600000000}})
		now := time.Unix(100, 0)
		source := bytes.Repeat([]byte{30, 60, 90, 0}, 640*height)
		draw := func() []byte {
			frame := append([]byte(nil), source...)
			renderVideoControls(frame, 640, height, state.presentation(m.Current().Detail, now), now)
			return frame
		}
		if !bytes.Equal(source, draw()) {
			t.Fatal("playing added instructions")
		}
		state.Paused = true
		if !bytes.Equal(source, draw()) {
			t.Fatal("pausing added an overlay")
		}
		state.RevealControls(now)
		if bytes.Equal(source, draw()) {
			t.Fatal("Up did not reveal controls")
		}
		now = now.Add(3 * time.Second)
		if !bytes.Equal(source, draw()) {
			t.Fatal("expired menu damaged the paused frame")
		}
		state.RevealControls(now)
		state.Paused = false
		state.HideControls()
		if !bytes.Equal(source, draw()) {
			t.Fatal("resume left instructions on video")
		}
	}
}

func TestVideoWaitingStates(t *testing.T) {
	state := playbackState{}
	state.PlayingVideo = true
	now := time.Unix(100, 0)
	if got := state.videoWaitLabel(now); got != "Loading..." {
		t.Fatal(got)
	}
	state.ProgressSeen, state.LastAdvance = true, now
	if got := state.videoWaitLabel(now); got != "" {
		t.Fatal(got)
	}
	if got := state.videoWaitLabel(now.Add(3 * time.Second)); got != "Buffering..." {
		t.Fatal(got)
	}
	state.BufferingKnown = true
	if got := state.videoWaitLabel(now.Add(10 * time.Second)); got != "" {
		t.Fatal(got)
	}
	state.Buffering = true
	if got := state.videoWaitLabel(now); got != "Buffering..." {
		t.Fatal(got)
	}
	state.Paused = true
	if got := state.videoWaitLabel(now); got != "" {
		t.Fatal("pause showed buffering", got)
	}
	state.ProgressSeen = false
	if got := state.videoWaitLabel(now); got != "" {
		t.Fatal("pause showed loading", got)
	}
}

func TestVideoSeekTargets(t *testing.T) {
	now := time.Unix(100, 0)
	for _, kind := range []string{"Movie", "Episode", "Video", "MusicVideo", "TvChannel", "LiveTvChannel", "Audio"} {
		m := New()
		state := playbackState{}
		state.PlayingVideo, state.ProgressSeen = kind != "Audio", true
		m.Stack = append(m.Stack, View{Detail: &jellyfin.Item{Type: kind, RunTimeTicks: 100 * 10000000}})
		state.PositionTicks = 2 * 10000000
		state.seekVideo(m.Current().Detail, "next", now)
		if kind == "Audio" || jellyfin.IsLive(*m.Current().Detail) {
			if state.SeekTarget != nil {
				t.Fatal("seek allowed for", kind)
			}
			continue
		}
		state.seekVideo(m.Current().Detail, "next", now.Add(100*time.Millisecond))
		if *state.SeekTarget != 62*10000000 || !state.SeekDeadline.Equal(now.Add(600*time.Millisecond)) {
			t.Fatal("seek did not accumulate or debounce")
		}
		state.seekVideo(m.Current().Detail, "previous", now)
		if *state.SeekTarget != 32*10000000 {
			t.Fatal("opposite direction did not subtract")
		}
		for range 4 {
			state.seekVideo(m.Current().Detail, "previous", now)
		}
		if *state.SeekTarget != 0 {
			t.Fatal("negative seek")
		}
		for range 4 {
			state.seekVideo(m.Current().Detail, "next", now)
		}
		if *state.SeekTarget != 99*10000000 {
			t.Fatal("seek beyond end")
		}
		state.SeekTarget, state.ProgressSeen = nil, false
		state.seekVideo(m.Current().Detail, "next", now)
		if state.SeekTarget != nil {
			t.Fatal("seek before playback ready")
		}
	}
}

func TestSeekOverlayShowsUpdatingDestination(t *testing.T) {
	for _, height := range []int{240, 288} {
		now := time.Unix(100, 0)
		m := New()
		state := playbackState{}
		state.PlayingVideo, state.ProgressSeen, state.BufferingKnown = true, true, true
		state.PositionTicks = 120 * 10000000
		m.Stack = append(m.Stack, View{Detail: &jellyfin.Item{Type: "Movie"}})
		source := bytes.Repeat([]byte{30, 60, 90, 0}, 640*height)
		draw := func() []byte {
			frame := append([]byte(nil), source...)
			renderVideoControls(frame, 640, height, state.presentation(m.Current().Detail, now), now)
			return frame
		}
		state.seekVideo(m.Current().Detail, "next", now)
		if !bytes.Equal(source, draw()) {
			t.Fatal("first press showed overlay")
		}
		state.seekVideo(m.Current().Detail, "next", now)
		second := draw()
		if bytes.Equal(source, second) || runtime(*state.SeekTarget) != "3:00" {
			t.Fatal("second press did not show destination")
		}
		state.seekVideo(m.Current().Detail, "next", now)
		if bytes.Equal(second, draw()) || runtime(*state.SeekTarget) != "3:30" {
			t.Fatal("Right did not update destination")
		}
		state.seekVideo(m.Current().Detail, "previous", now)
		if !bytes.Equal(second, draw()) {
			t.Fatal("Left did not restore previous destination")
		}
		state.SeekTarget = nil
		if !bytes.Equal(source, draw()) {
			t.Fatal("finished seek left an overlay")
		}
	}
}

func TestSeekInFlightReplacesDestinationOverlay(t *testing.T) {
	m := New()
	state := playbackState{}
	state.PlayingVideo, state.ProgressSeen, state.Paused = true, true, true
	m.Stack = append(m.Stack, View{Detail: &jellyfin.Item{Type: "Movie"}})
	now := time.Unix(100, 0)
	state.seekVideo(m.Current().Detail, "next", now)
	state.seekVideo(m.Current().Detail, "next", now)
	before := make([]byte, 640*240*4)
	renderVideoControls(before, 640, 240, state.presentation(m.Current().Detail, now), now)
	state.SeekInFlight = true
	if got := state.videoWaitLabel(now); got != "Seeking..." {
		t.Fatal(got)
	}
	after := make([]byte, len(before))
	renderVideoControls(after, 640, 240, state.presentation(m.Current().Detail, now), now)
	if bytes.Equal(before, after) {
		t.Fatal("destination remained during seek cleanup")
	}
	state.seekVideo(m.Current().Detail, "next", now)
	state.SeekInFlight = false
	retargeted := make([]byte, len(before))
	renderVideoControls(retargeted, 640, 240, state.presentation(m.Current().Detail, now), now)
	if bytes.Equal(after, retargeted) || runtime(*state.SeekTarget) != "1:30" {
		t.Fatal("retarget did not restore the updated destination")
	}
	state.SeekInFlight = true
	seekingAgain := make([]byte, len(before))
	renderVideoControls(seekingAgain, 640, 240, state.presentation(m.Current().Detail, now), now)
	if !bytes.Equal(after, seekingAgain) {
		t.Fatal("retarget did not return to seeking")
	}
}

// Compose a value snapshot over a fresh decoder frame for pixel comparisons.
func renderVideoControls(frame []byte, w, h int, p PlaybackPresentation, now time.Time) {
	ui.Composite(frame, renderVideoOverlay(w, h, p, now))
}
