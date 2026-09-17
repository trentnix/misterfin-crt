package rendering

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mistervision/internal/jellyfin"
	"mistervision/internal/media"
	"mistervision/internal/release"
	"mistervision/internal/ui"
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
		for _, name := range []string{"about", "about-update", "about-unavailable", "about-notes", "connecting", "quick-connect", "connection-error", "carousel", "list", "empty", "loading", "error", "exit", "notice", "details", "live-details", "photo", "photo-loading", "photo-error", "music", "music-paused", "video", "video-seek", "video-controls"} {
			m, art := benchmarkScene()
			presentation := PlaybackPresentation{}
			now := time.Unix(1800000000, 250000000).UTC()
			setup, artError := SetupPresentation{}, ""
			count := 42
			art.Photo = art.Primary
			item := jellyfin.Item{Name: "A long title for a sample movie or track", Type: "Movie", ProductionYear: 1988, CommunityRating: 7.8, RunTimeTicks: 6000000000, Overview: "A description that wraps across the detail screen."}
			item.UserData.PlaybackPositionTicks = 900000000
			switch name {
			case "connecting":
				setup = SetupPresentation{Kind: SetupConnecting, Title: "Connecting to Jellyfin", Message: "Checking your connection and saved sign-in."}
			case "quick-connect":
				setup = SetupPresentation{Kind: SetupApproval, Retry: "New code", Code: "123456", Title: "Quick Connect", Message: "In a signed-in Jellyfin client, open Quick Connect.\nEnter this code to approve MiSTerVision."}
			case "connection-error":
				setup = SetupPresentation{Kind: SetupFailure, Retry: "Retry", PathLabel: "Configuration file", Path: "/media/fat/mistervision/jellyfin.conf", Title: "Can't connect to Jellyfin", Message: "Check your server address and network connection.\nMake sure Jellyfin is running, then retry."}
			case "list":
				m.ListMode = true
			case "empty":
				m.ListMode = true
				m.Content.Page.Items = nil
			case "loading":
				m.ListMode = true
				m.Content.Loading = true
			case "error":
				m.ListMode = true
				m.Content.Error = "Could not load library"
			case "exit":
				m.ExitConfirm = true
			case "notice":
				m.Notice = "An informative message"
			case "details", "live-details":
				if name == "live-details" {
					item.Type = "TvChannel"
					item.CurrentProgram.Name = "Current program"
				}
				m.Root = false
				m.Content = Content{Detail: &item}
			case "photo", "photo-loading", "photo-error":
				item.Type = "Photo"
				m.Root = false
				m.Content = Content{Detail: &item}
				m.PhotoControlsVisible = true
				m.PhotoCount = "1/?"
				if name != "photo" {
					art.Photo = nil
				}
				if name == "photo-error" {
					artError = "unavailable"
					m.Notice = "Could not load photo"
				}
			case "music", "music-paused":
				item.Type = "Audio"
				m.Root = false
				m.Content = Content{Detail: &item}
				m.Audio = true
				presentation.Active = true
				presentation.Audio = true
				presentation.PositionTicks = 900000000
				presentation.ControlsVisible = true
				presentation.Paused = name == "music-paused"
			case "video", "video-seek", "video-controls":
				m.Root = false
				m.Content = Content{Detail: &item}
				presentation.Active = true
				presentation.WaitLabel = "Loading..."
				presentation.PositionTicks = 900000000
				if name == "video-seek" {
					target := int64(1200000000)
					presentation.HasDestination = true
					presentation.DestinationTicks = target
					presentation.ShowDestination = true
				}
				if name == "video-controls" {
					presentation.ControlsVisible = true
					presentation.WaitLabel = ""
				}
			}
			if m.Content.Detail != nil {
				presentation.Title = m.Content.Detail.Name
				presentation.DurationTicks = m.Content.Detail.RunTimeTicks
				presentation.Seekable = !media.IsLive(*m.Content.Detail)
				m.Content.CanResume = m.Content.Detail.Type == "Movie"
			}

			scene := testScene(m, presentation, setup, art, artError, now)
			scene.LibraryCount = &count
			if name == "about" || name == "about-update" || name == "about-unavailable" || name == "about-notes" {
				scene.About = AboutPresentation{Visible: true, Build: release.Build{Version: "v1.0.0", Revision: "abcdef123"}, Checked: true}
				if name == "about-update" || name == "about-notes" {
					scene.About.Release = release.Status{Latest: "v1.1.0", Available: true}
				}
				if name == "about-unavailable" {
					scene.About.Message = "No public release available."
				}
				if name == "about-notes" {
					scene.About.NotesVisible = true
					scene.About.CanInstall = true
					scene.About.Release.HasBundle = true
					scene.About.Notes = ReleaseNotes("A smoother CRT experience.\n\n- Improved browsing and playback.\n- Settings and sign-in are preserved.", 640)
				}
			}
			pixels := renderScene(ui.New(640, height), nil, scene, Animation{Seconds: 2.5, TitleSeconds: 3, Selection: 0.4, Row: 0.5})
			sum := sha256.New()
			sum.Write(pixels)
			if scene.Video {
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
