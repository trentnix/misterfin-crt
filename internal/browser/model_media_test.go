package browser

import (
	"testing"
	"time"

	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/jellyfin"
)

func TestAdjacentSelectionAndReturnRestoreParent(t *testing.T) {
	for _, kind := range []string{"Audio", "Photo"} {
		t.Run(kind, func(t *testing.T) {
			m := New()
			m.Current().Location.Kind = "items"
			m.Current().Page.Items = []jellyfin.Item{{ID: "first", Type: kind}}
			m.Key(control.Open)
			now := time.Unix(100, 0)
			m.StartMusicQueue()
			m.TogglePhotoControls(now)
			parent, ok := m.Parent()
			if !ok {
				t.Fatal("detail has no parent")
			}
			parent.Start, parent.Selected, parent.Target, parent.Scroll = 64, 8, 72, 3
			item := jellyfin.Item{ID: "neighbor", Name: "Next item", Type: kind}
			parent.Page.Items = make([]jellyfin.Item, 16)
			parent.Page.Items[8] = item
			before, _ := m.Parent()
			if before.Start != 0 || before.Selected != 0 || m.Current().Detail.ID != "first" {
				t.Fatal("preparing a neighbor changed visible navigation")
			}
			if !m.SelectAdjacent(parent, item) {
				t.Fatal("neighbor was not selected")
			}
			item.Name = "changed outside model"
			if m.Current().Title != "Next item" || m.Current().Detail.Name != "Next item" || len(m.Stack) != 2 {
				t.Fatal("detail identity or stack changed incorrectly")
			}
			if kind == "Photo" && !m.PhotoControlsVisible(now) {
				t.Fatal("adjacent photo lost the open menu")
			}
			if kind == "Audio" && !m.MusicQueueActive() {
				t.Fatal("adjacent track ended the queue")
			}
			stale := m.Load(0)
			m.Notice = "notice must not intercept a direct return"
			if !m.ReturnToParent() || m.Notice != "" || m.MusicQueueActive() || m.PhotoControlsVisible(now) {
				t.Fatal("return retained media presentation state")
			}
			v := m.Current()
			if v.Start != 64 || v.Selected != 8 || v.Scroll != 3 || v.Target != 72 || v.Loading {
				t.Fatalf("return lost the parent position: %+v", v)
			}
			if m.Apply(*stale, jellyfin.Page{}, nil) {
				t.Fatal("return accepted a stale listing")
			}
			generation := m.Generation
			if m.ReturnToParent() || m.Generation != generation || m.ExitConfirm {
				t.Fatal("direct return at root changed navigation")
			}
		})
	}
}

func TestPhotoControlsAreIndependentOfPlayback(t *testing.T) {
	f := newControllerFixture(t)
	f.c.Key(control.ToggleControls, f.now)
	f.c.Key(control.SeekForward, f.now)
	f.c.Key(control.SeekForward, f.now)
	playback := f.c.Snapshot(f.now)
	m := New()
	m.Current().Location.Kind = "items"
	m.Current().Page.Items = []jellyfin.Item{{ID: "photo", Type: "Photo"}}
	m.Key(control.Open)
	scene := func() Scene {
		return sceneFromModel(m, f.c.Snapshot(f.now), SetupPresentation{}, selectionData{}, "", f.now)
	}
	if scene().PhotoControlsVisible {
		t.Fatal("photo inherited the playback menu")
	}
	m.TogglePhotoControls(f.now)
	if !scene().PhotoControlsVisible || m.PhotoControlsVisible(f.now.Add(3*time.Second)) {
		t.Fatal("photo menu did not use its own timeout")
	}
	m.TogglePhotoControls(f.now)
	if scene().PhotoControlsVisible {
		t.Fatal("second Up did not hide the photo menu")
	}
	m.TogglePhotoControls(f.now)
	m.Key(control.Back)
	m.Key(control.Open)
	if scene().PhotoControlsVisible {
		t.Fatal("reopened photo retained its old menu")
	}
	if got := f.c.Snapshot(f.now); got != playback {
		t.Fatalf("photo navigation changed playback: got %+v, want %+v", got, playback)
	}
}
