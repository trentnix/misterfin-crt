package playback

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"misterfin-crt/internal/diagnostics"
	"misterfin-crt/internal/jellyfin"
)

func TestPlaybackDiagnosticsRecordMilestonesWithoutMediaSecrets(t *testing.T) {
	dir := t.TempDir()
	player := filepath.Join(dir, "player")
	// The raw decoder output deliberately contains a private URL. It must remain discarded.
	script := "#!/bin/sh\nprintf 'https://private-player/?ApiKey=decoder-secret\nANS_VIDEO_STARTED=true\nANS_BUFFERING=false\nANS_TIME_POSITION=3\n'\nwhile IFS= read -r command; do :; done\n"
	if err := os.WriteFile(player, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Items/item":
			fmt.Fprint(w, `{"Id":"item","Name":"title-secret","Type":"Movie","RunTimeTicks":1000000000}`)
		case "/Videos/item/stream":
			fmt.Fprint(w, "media")
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
	defer server.Close()
	path := filepath.Join(dir, "debug.log")
	log, err := diagnostics.Open(diagnostics.Config{Enabled: true, Path: path, MaxBytes: 32768})
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	client := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{Token: "token-secret"})
	client.Diagnostics = log
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first, position, buffer := make(chan struct{}, 1), make(chan struct{}, 1), make(chan struct{}, 1)
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		done <- Run(ctx, client, Config{VideoDecoder: DecoderConfig{Player: player}, Width: 640, Height: 480}, Request{
			Item: jellyfin.Item{ID: "item", Type: "Movie"},
			Callbacks: Callbacks{VideoStarted: func() { first <- struct{}{} }, Position: func(int64) {
				select {
				case position <- struct{}{}:
				default:
				}
			}, Buffering: func(bool) { buffer <- struct{}{} }},
		})
	}()
	defer func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("playback did not exit")
		}
	}()
	for _, ch := range []chan struct{}{first, position, buffer} {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatal("missing playback milestone")
		}
	}
	cancel()
	// Wait for cleanup before flushing the borrowed logger.
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("playback did not stop")
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	for _, event := range []string{"playback.start", "playback.prepared", "playback.first-frame", "playback.first-position", "playback.buffering", "playback.end"} {
		if !strings.Contains(string(data), event) {
			t.Fatalf("missing %s: %s", event, data)
		}
	}
	for _, secret := range []string{"title-secret", "token-secret", "decoder-secret", "private-player", server.URL} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
}
