package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"misterfin-go/internal/jellyfin"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestCleanMusicPauseAndControlTimeout(t *testing.T) {
	m := New()
	m.PlayingAudio = true
	m.Stack = append(m.Stack, View{Detail: &jellyfin.Item{Type: "Audio", Name: "Track", RunTimeTicks: 100000000}})
	now := time.Unix(100, 0)
	frame := func() []byte { return render(640, 240, m, "", Artwork{}, "", Animation{}, now) }
	playing := frame()
	m.Paused = true
	if !bytes.Equal(playing, frame()) {
		t.Fatal("pause added an overlay")
	}
	if !m.RevealControls(now) {
		t.Fatal("first navigation did not reveal")
	}
	if bytes.Equal(playing, frame()) {
		t.Fatal("controls not visible")
	}
	if m.RevealControls(now.Add(time.Second)) {
		t.Fatal("second navigation consumed")
	}
	now = now.Add(4 * time.Second)
	if m.ControlsVisible(now) {
		t.Fatal("controls did not expire")
	}
	if !bytes.Equal(playing, frame()) {
		t.Fatal("overlay remained after expiry")
	}
	m.RevealControls(now)
	m.HideControls()
	if m.ControlsVisible(now) {
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
	if err != nil || item == nil || item.ID != "65" || next.Start != 64 || next.Selected != 1 {
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
	if err != nil || item == nil || item.ID != "64" || next.Selected != 0 {
		t.Fatal("album queue did not cross page")
	}
}

func TestVideoControlsRestoreCleanFrame(t *testing.T) {
	for _, height := range []int{240, 288} {
		m := New()
		m.PlayingVideo = true
		m.ProgressSeen, m.BufferingKnown = true, true
		m.Stack = append(m.Stack, View{Detail: &jellyfin.Item{Type: "Movie", Name: "Movie", RunTimeTicks: 600000000}})
		now := time.Unix(100, 0)
		source := bytes.Repeat([]byte{30, 60, 90, 0}, 640*height)
		draw := func() []byte {
			frame := append([]byte(nil), source...)
			renderVideoControls(frame, 640, height, m, now)
			return frame
		}
		if !bytes.Equal(source, draw()) {
			t.Fatal("playing added instructions")
		}
		m.Paused = true
		if !bytes.Equal(source, draw()) {
			t.Fatal("pausing added an overlay")
		}
		m.RevealControls(now)
		if bytes.Equal(source, draw()) {
			t.Fatal("Up did not reveal controls")
		}
		now = now.Add(3 * time.Second)
		if !bytes.Equal(source, draw()) {
			t.Fatal("expired menu damaged the paused frame")
		}
		m.RevealControls(now)
		m.Paused = false
		m.HideControls()
		if !bytes.Equal(source, draw()) {
			t.Fatal("resume left instructions on video")
		}
	}
}

func TestVideoWaitingStates(t *testing.T) {
	m := New()
	m.PlayingVideo = true
	now := time.Unix(100, 0)
	if got := m.videoWaitLabel(now); got != "Loading..." {
		t.Fatal(got)
	}
	m.ProgressSeen, m.LastAdvance = true, now
	if got := m.videoWaitLabel(now); got != "" {
		t.Fatal(got)
	}
	if got := m.videoWaitLabel(now.Add(3 * time.Second)); got != "Buffering..." {
		t.Fatal(got)
	}
	m.BufferingKnown = true
	if got := m.videoWaitLabel(now.Add(10 * time.Second)); got != "" {
		t.Fatal(got)
	}
	m.Buffering = true
	if got := m.videoWaitLabel(now); got != "Buffering..." {
		t.Fatal(got)
	}
	m.Paused = true
	if got := m.videoWaitLabel(now); got != "" {
		t.Fatal("pause showed buffering", got)
	}
	m.ProgressSeen = false
	if got := m.videoWaitLabel(now); got != "" {
		t.Fatal("pause showed loading", got)
	}
}

func TestVideoSeekTargets(t *testing.T) {
	now := time.Unix(100, 0)
	for _, kind := range []string{"Movie", "Episode", "Video", "MusicVideo", "TvChannel", "LiveTvChannel", "Audio"} {
		m := New()
		m.PlayingVideo, m.ProgressSeen = kind != "Audio", true
		m.Stack = append(m.Stack, View{Detail: &jellyfin.Item{Type: kind, RunTimeTicks: 100 * 10000000}})
		m.PositionTicks = 2 * 10000000
		m.seekVideo("next", now)
		if kind == "Audio" || jellyfin.IsLive(*m.Current().Detail) {
			if m.SeekTarget != nil {
				t.Fatal("seek allowed for", kind)
			}
			continue
		}
		m.seekVideo("next", now.Add(100*time.Millisecond))
		if *m.SeekTarget != 62*10000000 || !m.SeekDeadline.Equal(now.Add(600*time.Millisecond)) {
			t.Fatal("seek did not accumulate or debounce")
		}
		m.seekVideo("previous", now)
		if *m.SeekTarget != 32*10000000 {
			t.Fatal("opposite direction did not subtract")
		}
		for range 4 {
			m.seekVideo("previous", now)
		}
		if *m.SeekTarget != 0 {
			t.Fatal("negative seek")
		}
		for range 4 {
			m.seekVideo("next", now)
		}
		if *m.SeekTarget != 99*10000000 {
			t.Fatal("seek beyond end")
		}
		m.SeekTarget, m.ProgressSeen = nil, false
		m.seekVideo("next", now)
		if m.SeekTarget != nil {
			t.Fatal("seek before playback ready")
		}
	}
}

func TestSeekOverlayShowsUpdatingDestination(t *testing.T) {
	for _, height := range []int{240, 288} {
		now := time.Unix(100, 0)
		m := New()
		m.PlayingVideo, m.ProgressSeen, m.BufferingKnown = true, true, true
		m.PositionTicks = 120 * 10000000
		m.Stack = append(m.Stack, View{Detail: &jellyfin.Item{Type: "Movie"}})
		source := bytes.Repeat([]byte{30, 60, 90, 0}, 640*height)
		draw := func() []byte {
			frame := append([]byte(nil), source...)
			renderVideoControls(frame, 640, height, m, now)
			return frame
		}
		m.seekVideo("next", now)
		if !bytes.Equal(source, draw()) {
			t.Fatal("first press showed overlay")
		}
		m.seekVideo("next", now)
		second := draw()
		if bytes.Equal(source, second) || runtime(*m.SeekTarget) != "3:00" {
			t.Fatal("second press did not show destination")
		}
		m.seekVideo("next", now)
		if bytes.Equal(second, draw()) || runtime(*m.SeekTarget) != "3:30" {
			t.Fatal("Right did not update destination")
		}
		m.seekVideo("previous", now)
		if !bytes.Equal(second, draw()) {
			t.Fatal("Left did not restore previous destination")
		}
		m.SeekTarget = nil
		if !bytes.Equal(source, draw()) {
			t.Fatal("finished seek left an overlay")
		}
	}
}

func TestSeekInFlightReplacesDestinationOverlay(t *testing.T) {
	m := New()
	m.PlayingVideo, m.ProgressSeen, m.Paused = true, true, true
	m.Stack = append(m.Stack, View{Detail: &jellyfin.Item{Type: "Movie"}})
	now := time.Unix(100, 0)
	m.seekVideo("next", now)
	m.seekVideo("next", now)
	before := make([]byte, 640*240*4)
	renderVideoControls(before, 640, 240, m, now)
	m.SeekInFlight = true
	if got := m.videoWaitLabel(now); got != "Seeking..." {
		t.Fatal(got)
	}
	after := make([]byte, len(before))
	renderVideoControls(after, 640, 240, m, now)
	if bytes.Equal(before, after) {
		t.Fatal("destination remained during seek cleanup")
	}
	m.seekVideo("next", now)
	m.SeekInFlight = false
	retargeted := make([]byte, len(before))
	renderVideoControls(retargeted, 640, 240, m, now)
	if bytes.Equal(after, retargeted) || runtime(*m.SeekTarget) != "1:30" {
		t.Fatal("retarget did not restore the updated destination")
	}
	m.SeekInFlight = true
	seekingAgain := make([]byte, len(before))
	renderVideoControls(seekingAgain, 640, 240, m, now)
	if !bytes.Equal(after, seekingAgain) {
		t.Fatal("retarget did not return to seeking")
	}
}
