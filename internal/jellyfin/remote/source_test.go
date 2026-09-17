package remote

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"github.com/coder/websocket"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"mistervision/internal/jellyfin"
	"mistervision/internal/media"
	"mistervision/internal/remote"
)

func TestSourceHTTPAndTLS(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(map[bool]string{false: "HTTP", true: "HTTPS"}[secure], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			registered := make(chan struct{}, 1)
			var registrations atomic.Int32
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Jellyfin can reject query-only socket authentication. Require
				// the same authenticated identity for the upgrade and API calls.
				wantAuth := `MediaBrowser Client="MiSTerVision", Device="MiSTerVision", Version="v2.3.4", DeviceId="device", Token="token"`
				if r.Header.Get("Authorization") != wantAuth {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				switch r.URL.Path {
				case "/jellyfin/Sessions/Capabilities/Full":
					var caps struct {
						PlayableMediaTypes, SupportedCommands []string
						SupportsMediaControl                  bool
					}
					if json.NewDecoder(r.Body).Decode(&caps) != nil || !caps.SupportsMediaControl || len(caps.PlayableMediaTypes) != 2 {
						t.Error("invalid capabilities")
					}
					for _, name := range caps.SupportedCommands {
						if name == "SetVolume" || name == "TakeScreenshot" {
							t.Error("unsupported capability")
						}
					}
					registrations.Add(1)
					registered <- struct{}{}
					w.WriteHeader(204)
				case "/jellyfin/socket":
					if r.URL.Query().Get("api_key") != "token" || r.URL.Query().Get("deviceId") != "device" {
						t.Error("missing socket credentials")
					}
					conn, err := websocket.Accept(w, r, nil)
					if err != nil {
						return
					}
					defer conn.CloseNow()
					select {
					case <-registered:
					case <-ctx.Done():
						return
					}
					_ = conn.Write(ctx, websocket.MessageText, []byte(`{"MessageType":"Playstate","Data":{"Command":"Pause"}}`))
					_, _, _ = conn.Read(ctx)
				default:
					t.Error("unexpected endpoint", r.URL.Path)
					w.WriteHeader(404)
				}
			})
			server := httptest.NewUnstartedServer(handler)
			if secure {
				server.StartTLS()
			} else {
				server.Start()
			}
			defer server.Close()
			client := jellyfin.NewClient(jellyfin.Config{Server: server.URL + "/jellyfin"}, jellyfin.Session{Token: "token", DeviceID: "device"})
			client.Version = "v2.3.4"
			if secure {
				roots := server.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
				client.HTTP.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
			}
			source := New(client)
			source.retry = time.Millisecond
			received := make(chan remote.Command, 2)
			done := make(chan struct{})
			go func() {
				defer close(done)
				source.Run(ctx, func(c remote.Command) {
					select {
					case received <- c:
					case <-ctx.Done():
					}
				})
			}()
			select {
			case c := <-received:
				if c.Kind != remote.Pause {
					t.Fatal(c)
				}
			case <-ctx.Done():
				t.Fatal("no command")
			}
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("source did not stop")
			}
			if registrations.Load() != 1 {
				t.Fatal("capabilities not registered")
			}
		})
	}
}

func TestTLSRejectsUntrustedAndRedirect(t *testing.T) {
	var reached atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Add(1) }))
	defer target.Close()
	client := jellyfin.NewClient(jellyfin.Config{Server: target.URL}, jellyfin.Session{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	New(client).connect(ctx, func(remote.Command) { t.Error("unexpected command") })
	if reached.Load() != 0 {
		t.Fatal("untrusted certificate accepted")
	}
	redirected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirected.Close()
	client = jellyfin.NewClient(jellyfin.Config{Server: redirected.URL, InsecureTLS: true}, jellyfin.Session{})
	New(client).connect(ctx, func(remote.Command) { t.Error("unexpected command") })
	if reached.Load() != 0 {
		t.Fatal("socket followed redirect")
	}
}

func TestPublishedQueueReportsOccurrences(t *testing.T) {
	reports := make(chan jellyfin.PlayState, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p jellyfin.PlayState
		_ = json.NewDecoder(r.Body).Decode(&p)
		reports <- p
		w.WriteHeader(204)
	}))
	defer server.Close()
	client := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{})
	New(client).Publish(remote.QueueState{Entries: []remote.Entry{{ID: "a", Key: "1"}, {ID: "a", Key: "2"}}, Current: "2", Repeat: remote.RepeatAll, Shuffled: true})
	if err := client.ReportPlaying(context.Background(), "progress", media.PlayState{ItemID: "a"}); err != nil {
		t.Fatal(err)
	}
	p := <-reports
	if len(p.NowPlayingQueue) != 2 || p.PlaylistItemID != "2" || p.RepeatMode != "RepeatAll" || p.PlaybackOrder != "Shuffle" {
		t.Fatalf("missing queue: %+v", p)
	}
	if err := client.ReportPlaying(context.Background(), "stopped", media.PlayState{ItemID: "old"}); err != nil {
		t.Fatal(err)
	}
	if p := <-reports; len(p.NowPlayingQueue) != 0 {
		t.Fatal("old decoder received new queue")
	}
}

func TestReconnectRegistersCapabilitiesAgain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	registered := make(chan struct{}, 4)
	var registrations atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Sessions/Capabilities/Full" {
			registrations.Add(1)
			registered <- struct{}{}
			w.WriteHeader(204)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		select {
		case <-registered:
		case <-ctx.Done():
			return
		}
		if registrations.Load() == 1 {
			return
		}
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"MessageType":"Playstate","Data":{"Command":"Stop"}}`))
		_, _, _ = conn.Read(ctx)
	}))
	defer server.Close()
	source := New(jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{}))
	source.retry = time.Millisecond
	received := false
	source.Run(ctx, func(command remote.Command) { received = command.Kind == remote.Stop; cancel() })
	if !received || registrations.Load() < 2 {
		t.Fatal("reconnect failed")
	}
}
