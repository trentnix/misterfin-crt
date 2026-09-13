package browser

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"misterfin-go/internal/jellyfin"
	"misterfin-go/internal/ui"
)

// TestRenderScreenPixels protects screen layout across structural changes.
// To accept an intentional visual change, run with UPDATE_RENDER_GOLDEN=1.
func TestRenderScreenPixels(t *testing.T) {
	path := filepath.Join("testdata", "render_screens.json")
	want := map[string]string{}
	if os.Getenv("UPDATE_RENDER_GOLDEN") != "1" {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(data, &want); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]string{}
	for _, height := range []int{240, 288} {
		for _, name := range []string{"connecting", "quick-connect", "connection-error", "carousel", "list", "empty", "loading", "error", "exit", "notice", "details", "live-details", "photo", "photo-loading", "photo-error", "music", "music-paused", "video", "video-seek", "video-controls"} {
			m, art := benchmarkScene()
			state := playbackState{}
			now := time.Unix(1800000000, 250000000).UTC()
			status, artError := "", ""
			count := 42
			art.Photo = art.Primary
			item := jellyfin.Item{Name: "A long title for a sample movie or track", Type: "Movie", ProductionYear: 1988, CommunityRating: 7.8, RunTimeTicks: 6000000000, Overview: "A description that wraps across the detail screen."}
			item.UserData.PlaybackPositionTicks = 900000000
			switch name {
			case "connecting":
				status = "Connecting to Jellyfin..."
			case "quick-connect":
				status = "Quick Connect: 123456\nWaiting"
			case "connection-error":
				status = "Connection refused"
			case "list":
				m.ListMode = true
			case "empty":
				m.ListMode = true
				m.Current().Page.Items = nil
			case "loading":
				m.ListMode = true
				m.Current().Loading = true
			case "error":
				m.ListMode = true
				m.Current().Error = "Could not load library"
			case "exit":
				m.ExitConfirm = true
			case "notice":
				m.Notice = "An informative message"
			case "details", "live-details":
				if name == "live-details" {
					item.Type = "TvChannel"
					item.CurrentProgram.Name = "Current program"
				}
				m.Stack = append(m.Stack, View{Detail: &item})
			case "photo", "photo-loading", "photo-error":
				item.Type = "Photo"
				m.Stack = append(m.Stack, View{Detail: &item})
				m.TogglePhotoControls(now)
				if name != "photo" {
					art.Photo = nil
				}
				if name == "photo-error" {
					artError = "unavailable"
					m.Notice = "Could not load photo"
				}
			case "music", "music-paused":
				item.Type = "Audio"
				m.Stack = append(m.Stack, View{Detail: &item})
				m.StartMusicQueue()
				state.PositionTicks = 900000000
				state.RevealControls(now)
				state.Paused = name == "music-paused"
			case "video", "video-seek", "video-controls":
				m.Stack = append(m.Stack, View{Detail: &item})
				state.PlayingVideo = true
				state.PositionTicks = 900000000
				if name == "video-seek" {
					target := int64(1200000000)
					state.SeekTarget = &target
					state.SeekPresses = 2
				}
				if name == "video-controls" {
					state.RevealControls(now)
					state.ProgressSeen = true
					state.LastAdvance = now
				}
			}
			presentation := state.presentation(m.Current().Detail, now)
			presentation.Active = state.PlayingVideo || m.MusicQueueActive()
			presentation.Audio = m.MusicQueueActive()
			scene := sceneFromModel(m, presentation, status, selectionData{artwork: art, count: &count}, artError, now)
			pixels := renderScene(ui.New(640, height), nil, scene, Animation{Seconds: 2.5, TitleSeconds: 3, Selection: 0.4, Row: 0.5})
			sum := sha256.New()
			sum.Write(pixels)
			if state.PlayingVideo {
				sum.Write(renderVideoOverlay(640, height, presentation, now))
			}
			key := fmt.Sprintf("%d/%s", height, name)
			got[key] = fmt.Sprintf("%x", sum.Sum(nil))
			if os.Getenv("UPDATE_RENDER_GOLDEN") != "1" && want[key] != got[key] {
				t.Errorf("pixels changed for %s: got %s, want %s", key, got[key], want[key])
			}
		}
	}
	if os.Getenv("UPDATE_RENDER_GOLDEN") == "1" {
		data, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, append(data, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
}
