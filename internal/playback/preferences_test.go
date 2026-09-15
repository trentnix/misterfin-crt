package playback

import (
	"context"
	"encoding/json"
	"misterfin-crt/internal/diagnostics"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"misterfin-crt/internal/jellyfin"
	nativeplayer "misterfin-crt/internal/player/mplayer"
)

func preferenceTracks() VideoTracks {
	return VideoTracks{SourceID: "source", TrackOptions: TrackOptions{
		Picture: PictureZoom43, Selection: jellyfin.TrackSelection{AudioIndex: 4, SubtitleIndex: 12}},
		Streams: []jellyfin.MediaStream{
			{Type: "Audio", Index: 4, Codec: "aac", Language: "jpn"},
			{Type: "Subtitle", Index: 12, Codec: "ass", Language: "eng"},
		}}
}

func TestPreferencesPersistAndIsolateAccountsAndItems(t *testing.T) {
	dir := t.TempDir()
	c := jellyfin.NewClient(jellyfin.Config{Server: "http://server"}, jellyfin.Session{UserID: "user", Token: "private-token"})
	key := preferenceKey(c, "movie")
	tracks := preferenceTracks()
	p := NewPreferences(dir, nil)
	for _, mode := range []PictureMode{PictureZoom43, PictureOriginal, PictureZoom43} {
		tracks.Picture = mode
		p.save(key, tracks)
		if got := p.load(key); got == nil || got.restore(tracks).Picture != mode {
			t.Fatal("immediate reopen lost the latest choice")
		}
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	p = NewPreferences(dir, nil)
	defer p.Close()
	got := p.load(key)
	if got == nil || got.restore(tracks) != tracks.TrackOptions {
		t.Fatal("app restart lost picture, audio, or subtitle choice")
	}
	for _, tc := range [][3]string{{"http://other", "user", "movie"}, {"http://server", "other", "movie"}, {"http://server", "user", "episode"}} {
		other := jellyfin.NewClient(jellyfin.Config{Server: tc[0]}, jellyfin.Session{UserID: tc[1]})
		if p.load(preferenceKey(other, tc[2])) != nil {
			t.Fatal("choices leaked between accounts or items")
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "playback", key+".json"))
	if err != nil || strings.Contains(string(data), "private-token") {
		t.Fatal("could not read choices or token was persisted")
	}
}

func TestSavedTracksFallBackWhenSourceOrStreamChanges(t *testing.T) {
	p := NewPreferences(t.TempDir(), nil)
	defer p.Close()
	original := preferenceTracks()
	p.save("item", original)
	saved := p.load("item")
	for _, change := range []string{"source", "missing-audio", "replaced-subtitle"} {
		t.Run(change, func(t *testing.T) {
			current := preferenceTracks()
			wantAudio, wantSubtitle := 4, 12
			switch change {
			case "source":
				current.SourceID = "replacement-file"
				wantAudio, wantSubtitle = -1, -1
			case "missing-audio":
				current.Streams = current.Streams[1:]
				wantAudio = -1
			case "replaced-subtitle":
				current.Streams[1].Language = "fra"
				wantSubtitle = -1
			}
			got, err := videoTracks(jellyfin.Item{ID: "item", MediaSources: []jellyfin.MediaSource{{ID: current.SourceID, MediaStreams: current.Streams}}}, trackPreparation{saved: saved, clientSubtitles: true})
			if err != nil || got.Picture != PictureZoom43 || got.Selection.AudioIndex != wantAudio || got.Selection.SubtitleIndex != wantSubtitle || got.Text != nil {
				t.Fatalf("bad saved-track fallback: %+v, %v", got, err)
			}
		})
	}
}

func TestPreferencesOffAndDefaultReplacePreviousSelections(t *testing.T) {
	dir := t.TempDir()
	p := NewPreferences(dir, nil)
	tracks := preferenceTracks()
	p.save("item", tracks)
	tracks.TrackOptions = TrackOptions{Selection: jellyfin.TrackSelection{AudioIndex: -1, SubtitleIndex: -1}}
	p.save("item", tracks)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	p = NewPreferences(dir, nil)
	defer p.Close()
	if got := p.load("item"); got == nil || got.restore(tracks) != tracks.TrackOptions {
		t.Fatal("Off, server default, or Original reverted to an earlier choice")
	}
}

func TestPreferencesInvalidOrUnwritableStorage(t *testing.T) {
	for _, data := range []string{"broken", `{"version":99}`, `{"version":1,"picture":99}`, strings.Repeat(" ", 16384) + `{"version":1}`} {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "playback"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "playback", "item.json"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		p := NewPreferences(dir, nil)
		if p.load("item") != nil {
			t.Error("damaged record did not fall back to defaults")
		}
		p.Close()
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "playback"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	p := NewPreferences(dir, nil)
	p.save("item", preferenceTracks())
	if p.load("item") == nil {
		t.Fatal("unwritable storage lost in-memory choices")
	}
	if p.Close() == nil {
		t.Fatal("failed persistence was not reported")
	}
}

func TestUnstartedAndNonVideoSessionsDoNotSaveChoices(t *testing.T) {
	p := NewPreferences(t.TempDir(), nil)
	defer p.Close()
	for _, tc := range []struct {
		started, live bool
		kind          string
	}{{false, false, "Movie"}, {true, true, "TvChannel"}, {true, false, "Audio"}} {
		s := playbackSession{preferences: p, preferenceKey: "item", tracks: preferenceTracks(), started: tc.started, liveTV: tc.live, item: jellyfin.Item{Type: tc.kind}}
		s.rememberChoices()
		if p.load("item") != nil {
			t.Fatal("unstarted replacement, Live TV, or music saved video choices")
		}
	}
}

func TestResumeRestoresChoicesInDecoderAndStream(t *testing.T) {
	tracks := preferenceTracks()
	// Burn-in lets this test inspect subtitle restoration in the stream URL.
	tracks.Streams[1].Codec = "pgssub"
	item := jellyfin.Item{ID: "movie", Type: "Movie", RunTimeTicks: 9000000000,
		MediaSources: []jellyfin.MediaSource{{ID: tracks.SourceID, MediaStreams: append(tracks.Streams,
			jellyfin.MediaStream{Type: "Video", Width: 1920, Height: 1080, AspectRatio: "16:9"})}}}
	item.UserData.PlaybackPositionTicks = 600000000
	queries := make(chan string, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/Items/movie":
			json.NewEncoder(w).Encode(item)
		case strings.HasPrefix(r.URL.Path, "/Videos/"):
			queries <- r.URL.RawQuery
			w.Write([]byte("video"))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	c := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{UserID: "user"})
	dir := t.TempDir()
	path := filepath.Join(dir, "player")
	args := filepath.Join(dir, "args")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > '"+args+"'\nprintf 'ANS_TIME_POSITION=1\\n'\ncat /dev/fd/3 >/dev/null\n"), 0700); err != nil {
		t.Fatal(err)
	}
	run := func(p *Preferences, explicit *TrackOptions, start *int64) {
		t.Helper()
		err := Run(context.Background(), c, Config{Preferences: p, VideoDecoder: nativeplayer.Decoder{Player: path, Width: 640, Height: 240}, AudioDecoder: nativeplayer.Decoder{Player: path, Width: 640, Height: 240}, Height: 240}, Request{Item: item, Tracks: explicit, StartTicks: start, Callbacks: Callbacks{Position: func(int64) {}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	p := NewPreferences(dir, nil)
	run(p, &tracks.TrackOptions, nil)
	<-queries
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	p = NewPreferences(dir, nil)
	defer p.Close()
	// A prepared replacement canceled behind its start gate must not save
	// its explicit defaults over the choices used by the preceding decoder.
	ctx, cancel := context.WithCancel(context.Background())
	defaults := TrackOptions{Selection: jellyfin.TrackSelection{AudioIndex: -1, SubtitleIndex: -1}}
	if err := Run(ctx, c, Config{Preferences: p, VideoDecoder: nativeplayer.Decoder{Player: path, Width: 640, Height: 240}, AudioDecoder: nativeplayer.Decoder{Player: path, Width: 640, Height: 240}, Height: 240}, Request{Item: item, Tracks: &defaults, Start: make(chan struct{}), Callbacks: Callbacks{Ready: cancel, Position: func(int64) {}}}); err != nil {
		t.Fatal(err)
	}
	cancel()
	<-queries
	for _, start := range []*int64{nil, new(int64)} {
		run(p, nil, start)
		query := <-queries
		wantStart := "600000000"
		if start != nil {
			wantStart = "0"
		}
		for _, want := range []string{"audioStreamIndex=4", "subtitleStreamIndex=12", "subtitleMethod=Encode", "startTimeTicks=" + wantStart} {
			if !strings.Contains(query, want) {
				t.Fatalf("resume/restart query missing %s", want)
			}
		}
		data, err := os.ReadFile(args)
		if err != nil || !strings.Contains(string(data), "misterfin=640:240:1.777777778:1") {
			t.Fatal("decoder did not restore Zoom")
		}
	}
}

func TestPreferencesRetryAfterStorageRecovers(t *testing.T) {
	for _, action := range []string{"save-again", "close", "newer-choice"} {
		t.Run(action, func(t *testing.T) {
			dir := t.TempDir()
			blocked := filepath.Join(dir, "playback")
			if err := os.WriteFile(blocked, nil, 0600); err != nil {
				t.Fatal(err)
			}
			logPath := filepath.Join(dir, "diagnostics.log")
			trace, err := diagnostics.Open(diagnostics.Config{Enabled: true, Path: logPath, MaxBytes: 4096})
			if err != nil {
				t.Fatal(err)
			}
			defer trace.Close()
			p := NewPreferences(dir, trace)
			closed := false
			defer func() {
				if !closed {
					p.Close()
				}
			}()
			tracks := preferenceTracks()
			p.save("private-item", tracks)
			deadline := time.Now().Add(3 * time.Second)
			for {
				data, _ := os.ReadFile(logPath)
				if strings.Contains(string(data), "playback.preferences-write") {
					if strings.Contains(string(data), "private-item") {
						t.Fatal("failure log exposed item identity")
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("write failure was not logged")
				}
				time.Sleep(time.Millisecond)
			}
			if got := p.load("private-item"); got == nil || got.Picture != tracks.Picture {
				t.Fatal("failed write lost in-memory choice")
			}
			if err := os.Remove(blocked); err != nil {
				t.Fatal(err)
			}
			if action == "newer-choice" {
				tracks.Picture = PictureOriginal
			}
			if action != "close" {
				p.save("private-item", tracks)
			}
			if action == "save-again" {
				deadline = time.Now().Add(3 * time.Second)
				for {
					if _, err := os.Stat(filepath.Join(blocked, "private-item.json")); err == nil {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("unchanged save did not retry before Close")
					}
					time.Sleep(time.Millisecond)
				}
			}
			closeErr := p.Close()
			closed = true
			if closeErr != nil {
				t.Fatal(closeErr)
			}
			p2 := NewPreferences(dir, nil)
			defer p2.Close()
			if got := p2.load("private-item"); got == nil || got.restore(tracks) != tracks.TrackOptions {
				t.Fatal("recovered storage did not preserve the latest choice")
			}
		})
	}
}
