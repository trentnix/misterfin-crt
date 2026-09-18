package plex

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"mistervision/internal/media"
	"mistervision/internal/serverstate"
)

func liveID(dvr, channel string) string {
	return "live:" + dvr + ":" + base64.RawURLEncoding.EncodeToString([]byte(channel))
}

const testDVRs = `{"MediaContainer":{"Dvr":[{"key":"2","lineup":"lineup://test","epgIdentifier":"tv.plex.providers.epg.cloud:2","Lineup":[{"title":"Guide"}],"Device":[{"key":"1","state":"enabled","ChannelMapping":[{"channelKey":"ten","deviceIdentifier":"10.1","enabled":"1"},{"channelKey":"two-ten","deviceIdentifier":"2.10","enabled":"1"},{"channelKey":"two-two","deviceIdentifier":"2.2","enabled":"1"},{"channelKey":"disabled","deviceIdentifier":"1.1","enabled":"0"},{"channelKey":"protected","deviceIdentifier":"3.1","enabled":"1"}]},{"key":"3","state":"enabled","ChannelMapping":[{"channelKey":"two-two","deviceIdentifier":"2.2","enabled":"1"}]}]}]}}`

func TestLiveChannelDiscoveryAndPaging(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/playlists", "/library/all":
			fmt.Fprint(w, `{"MediaContainer":{"size":0,"totalSize":0}}`)
		case "/library/sections":
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","title":"Home Videos","type":"movie","scanner":"Plex Video Files Scanner"},{"key":"9","title":"Photos","type":"photo"}]}}`)
		case "/livetv/dvrs":
			fmt.Fprint(w, testDVRs)
		case "/tv.plex.providers.epg.cloud:2/grid":
			if r.URL.Query().Get("beginsAt<") == "" || r.URL.Query().Get("endsAt>") == "" || r.URL.Query().Has("beginsAt<=") {
				t.Error("invalid Plex guide time filters")
			}
			now := time.Now().Unix()
			fmt.Fprintf(w, `{"MediaContainer":{"Metadata":[{"title":"Episode","grandparentTitle":"Current show","Media":[{"channelIdentifier":"two-two","beginsAt":%d,"endsAt":%d}]},{"title":"Past show","Media":[{"channelIdentifier":"two-two","beginsAt":1,"endsAt":2}]}]}}`, now-100, now+100)
		case "/livetv/epg/channels":
			fmt.Fprint(w, `{"MediaContainer":{"Channel":[{"key":"two-two","identifier":"2.2","callSign":"PBS","thumb":"https://provider-static.plex.tv/logo.png"}]}}`)
		case "/media/grabbers/devices/1/channels", "/media/grabbers/devices/3/channels":
			fmt.Fprint(w, `{"MediaContainer":{"DeviceChannel":[{"identifier":"10.1","name":"News"},{"identifier":"protected","drm":true},{"identifier":"3.1","drm":true}]}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	libraries, err := c.Libraries(t.Context())
	if err != nil || len(libraries.Items) != 3 || libraries.Items[0].Name != "Home Videos" || libraries.Items[2].CollectionType != "livetv" {
		t.Fatalf("libraries = %+v, %v", libraries, err)
	}
	page, err := c.List(t.Context(), media.Location{Kind: "livetv"}, 0, 2)
	if err != nil || len(page.Items) != 2 || *page.TotalRecordCount != 3 {
		t.Fatalf("channels = %+v, %v", page, err)
	}
	if page.Items[0].Number != "2.2" || page.Items[1].Number != "2.10" || page.Items[0].Name != "PBS" {
		t.Fatalf("channel order/names: %+v", page.Items)
	}
	detail, err := c.PlaybackDetails(t.Context(), page.Items[0].ID)
	if err != nil || detail.Type != "TvChannel" || detail.ImageTags["Primary"] == "" || detail.CurrentProgram.Name != "Current show" {
		t.Fatalf("channel detail: %+v, %v", detail, err)
	}
	last, err := c.List(t.Context(), media.Location{Kind: "livetv"}, 2, 2)
	if err != nil || len(last.Items) != 1 || last.Items[0].Name != "News" {
		t.Fatalf("last page: %+v, %v", last, err)
	}
	count, err := c.LibraryCount(t.Context(), libraries.Items[2])
	if err != nil || *count != 3 {
		t.Fatalf("count: %v, %v", count, err)
	}
	mosaic, err := c.Mosaic(t.Context(), libraries.Items[2])
	if err != nil || len(mosaic.Items) != 3 {
		t.Fatalf("mosaic: %+v, %v", mosaic, err)
	}
	beyond, err := c.channelPage(t.Context(), math.MaxInt, 20)
	if err != nil || len(beyond.Items) != 0 {
		t.Fatal("page beyond end must be empty")
	}
}

func TestLiveDiscoveryWithoutGuideOrPermission(t *testing.T) {
	for _, status := range []int{200, 403, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/playlists", "/library/all":
					fmt.Fprint(w, `{"MediaContainer":{"size":0,"totalSize":0}}`)
				case "/library/sections":
					fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","type":"movie","title":"Movies"}]}}`)
				case "/livetv/dvrs":
					if status != 200 {
						w.WriteHeader(status)
					} else {
						fmt.Fprint(w, testDVRs)
					}
				default:
					w.WriteHeader(404)
				}
			})
			libraries, err := c.Libraries(t.Context())
			if err != nil || len(libraries.Items) == 0 {
				t.Fatalf("personal libraries lost: %v", err)
			}
			if status == 200 {
				page, err := c.channelPage(t.Context(), 0, 20)
				if err != nil || page.Items[0].Name != "Channel 2.2" {
					t.Fatalf("guide fallback: %+v, %v", page, err)
				}
			} else if len(libraries.Items) != 1 {
				t.Fatal("inaccessible Live TV was exposed")
			}
		})
	}
}

func TestInvalidLiveIDs(t *testing.T) {
	for _, id := range []string{"", "live:../2:YWJj", liveID("2", "../bad"), liveID("2", "bad%2fpath"), liveID("2", "bad\npath"), "live:2:!", liveID("2", ""), liveID("2", strings.Repeat("a", 513))} {
		if _, _, err := parseChannelID(id); err == nil {
			t.Fatalf("accepted invalid channel ID %q", id)
		}
	}
	if dvr, ch, err := parseChannelID(liveID("2", "BBC One HD")); err != nil || dvr != "2" || ch != "BBC One HD" {
		t.Fatal("XMLTV channel name rejected")
	}
}

const liveReply = `{"MediaContainer":{"MediaSubscription":[{"MediaGrabOperation":[{"Metadata":{"Media":[{"uuid":"live-source","width":720,"height":480,"aspectRatio":"1.78","Part":[{"Stream":[{"streamType":1,"index":0,"codec":"mpeg2video"},{"streamType":2,"index":1,"codec":"ac3"},{"streamType":3,"index":2,"codec":"eia_608"}]}]}]}}]}]}}`

func liveClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "private-token" {
			t.Error("missing token")
		}
		for _, key := range []string{"X-Plex-Token", "X-Plex-Client-Identifier", "X-Plex-Session-Identifier"} {
			if r.URL.Query().Has(key) {
				t.Error("private identity remained in URL")
			}
		}
		if r.Header.Get("X-Plex-Client-Identifier") == "device" || r.Header.Get("X-Plex-Client-Identifier") != r.Header.Get("X-Plex-Session-Identifier") {
			t.Error("live attempt lacks isolated consumer")
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return NewClient(Config{Server: server.URL}, serverstate.Session{DeviceID: "device", Token: "private-token"})
}

func TestLivePlaybackOwnershipAndReports(t *testing.T) {
	var mu sync.Mutex
	active := map[string]bool{}
	stopped := map[string]bool{}
	c := liveClient(t, func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Plex-Client-Identifier")
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/tune"):
			if r.Method != "POST" {
				t.Error("tune must POST")
			}
			active[id] = true
			fmt.Fprint(w, liveReply)
		case strings.HasSuffix(r.URL.Path, "/decision"):
			q := r.URL.Query()
			if q.Get("offset") != "-1" || q.Get("path") != "/livetv/sessions/live-source" || q.Get("protocol") != "http" || !strings.Contains(q.Get("X-Plex-Client-Profile-Extra"), "29.97002997002997") {
				t.Error("incorrect live transcode")
			}
			fmt.Fprint(w, `{"MediaContainer":{"generalDecisionCode":1001}}`)
		case strings.HasSuffix(r.URL.Path, "/start.mkv"):
			fmt.Fprint(w, "video")
		case r.URL.Path == "/:/timeline":
			if r.URL.Query().Get("key") != "/livetv/sessions/live-source" || r.URL.Query().Has("ratingKey") {
				t.Error("live report touched library history")
			}
		case strings.HasSuffix(r.URL.Path, "/stop"):
			stopped[id] = true
		case strings.HasPrefix(r.URL.Path, "/media/grabbers/operations/"):
			if r.Method != "DELETE" || r.URL.Path != "/media/grabbers/operations/channel-"+id {
				t.Error("cleanup not scoped to owner")
			}
			delete(active, id)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	})
	a, err := c.PrepareLive(t.Context(), media.LiveRequest{ChannelID: liveID("2", "channel"), MaxFrameRate: 30000.0 / 1001, AudioIndex: -1})
	if err != nil {
		t.Fatal(err)
	}
	if a.Streams[0].AspectRatio != "1.78" || a.Streams[0].Width != 720 || a.Streams[2].Codec != "eia_608" {
		t.Fatalf("source geometry/captions lost: %+v", a.Streams)
	}
	b, err := c.PrepareLive(t.Context(), media.LiveRequest{ChannelID: liveID("2", "channel"), MaxFrameRate: 30000.0 / 1001, AudioIndex: -1})
	if err != nil {
		t.Fatal(err)
	}
	body, err := c.OpenStream(t.Context(), b.URL)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(body)
	body.Close()
	if string(data) != "video" {
		t.Fatal("stream did not open")
	}
	for _, event := range []string{"start", "progress", "stopped"} {
		if err := a.Reports.ReportPlaying(t.Context(), event, media.PlayState{ItemID: liveID("2", "channel"), PositionTicks: 10000000}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Reports.SavePlaybackPosition(t.Context(), "not-a-library-id", 0, true); err != nil {
		t.Fatal(err)
	}
	if err := a.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	safe := len(active) == 1 && active[b.SessionID] && stopped[a.SessionID] && !stopped[b.SessionID]
	mu.Unlock()
	if !safe {
		t.Fatal("old cleanup disturbed replacement")
	}
	if err := b.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestLivePreparationFailuresReleaseConsumer(t *testing.T) {
	for _, failure := range []string{"cancel", "malformed", "missing-source", "refused-decision", "tuner-error"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			released := make(chan bool, 2)
			c := liveClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/tune"):
					switch failure {
					case "cancel":
						cancel()
						time.Sleep(5 * time.Millisecond)
						fmt.Fprint(w, liveReply)
					case "malformed":
						fmt.Fprint(w, `{`)
					case "missing-source":
						fmt.Fprint(w, `{"MediaContainer":{"Metadata":[]}}`)
					case "tuner-error":
						fmt.Fprint(w, `{"MediaContainer":{"status":-1}}`)
					default:
						fmt.Fprint(w, liveReply)
					}
				case strings.HasSuffix(r.URL.Path, "/decision"):
					fmt.Fprint(w, `{"MediaContainer":{"generalDecisionCode":2000}}`)
				case strings.HasSuffix(r.URL.Path, "/stop"), strings.HasPrefix(r.URL.Path, "/media/grabbers/operations/"):
					released <- true
					w.WriteHeader(404)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
				}
			})
			if _, err := c.PrepareLive(ctx, media.LiveRequest{ChannelID: liveID("2", "channel"), MaxFrameRate: 30, AudioIndex: -1}); err == nil {
				t.Fatal("failed negotiation succeeded")
			}
			if len(released) != 2 {
				t.Fatalf("released %d resources, want two", len(released))
			}
		})
	}
}

func TestLiveRejectsInvalidCadenceBeforeTuning(t *testing.T) {
	c := testClient(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid request reached server") })
	for _, fps := range []float64{-1, 0, math.NaN(), math.Inf(1), 1000} {
		if _, err := c.PrepareLive(t.Context(), media.LiveRequest{ChannelID: liveID("2", "channel"), MaxFrameRate: fps, AudioIndex: -1}); err == nil {
			t.Fatal("invalid cadence accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.PrepareLive(ctx, media.LiveRequest{ChannelID: liveID("2", "channel"), MaxFrameRate: 30, AudioIndex: -1}); err != context.Canceled {
		t.Fatalf("cancellation = %v", err)
	}
}

func TestTunedVideoResponseFormats(t *testing.T) {
	for _, response := range []string{liveReply, strings.Replace(liveReply, `"Metadata":{`, `"Video":{`, 1), `{"MediaContainer":{"Metadata":[{"Media":[{"uuid":"flat-session","aspectRatio":1.33}]}]}}`} {
		if _, err := tunedVideo([]byte(response)); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"../another", "?secret", "https://other", ""} {
		if _, err := tunedVideo([]byte(fmt.Sprintf(`{"MediaContainer":{"Metadata":[{"Media":[{"uuid":%q}]}]}}`, id))); err == nil {
			t.Fatal("unsafe session accepted")
		}
	}
}

func TestLiveCleanupDoesNotWaitForTranscodeStop(t *testing.T) {
	detached := make(chan struct{})
	c := liveClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			close(detached)
			return
		}
		select {
		case <-detached:
		case <-r.Context().Done():
		}
	})
	owner := livePlayback{client: c, session: "test-consumer", operation: "/media/grabbers/operations/channel-test-consumer"}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := owner.release(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestLiveChannelLogoUsesServerProxy(t *testing.T) {
	const logo = "https://provider-static.plex.tv/channels/logo.png"
	called := false
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/photo/:/transcode" || r.URL.Query().Get("url") != logo {
			t.Error("logo bypassed configured server proxy")
		}
		w.WriteHeader(http.StatusNotFound)
	})
	_, _ = c.image(t.Context(), logo, 320, 240)
	if !called {
		t.Fatal("Plex channel logo rejected")
	}
	for _, source := range []string{"https://other.example/logo.png", "https://provider-static.plex.tv.evil.example/logo.png", "https://user:secret@provider-static.plex.tv/logo.png"} {
		called = false
		if _, err := c.image(t.Context(), source, 320, 240); err == nil || called {
			t.Fatal("untrusted external image source accepted")
		}
	}
}

func TestLiveTuneOutlivesMetadataTimeout(t *testing.T) {
	c := liveClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/tune") {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		time.Sleep(40 * time.Millisecond)
		fmt.Fprint(w, liveReply)
	})
	c.HTTP.Timeout = 5 * time.Millisecond
	owner := livePlayback{client: c, session: "consumer"}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	data, err := owner.tune(ctx, "2", "channel")
	if err != nil {
		t.Fatal("tune inherited metadata timeout", err)
	}
	if _, err := tunedVideo(data); err != nil {
		t.Fatal(err)
	}
	if c.HTTP.Timeout != 5*time.Millisecond {
		t.Fatal("tuner startup changed shared HTTP timeout")
	}
}

func TestLiveTuneDeadlineExplainsFailure(t *testing.T) {
	c := liveClient(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	owner := livePlayback{client: c, session: "consumer"}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err := owner.tune(ctx, "2", "channel")
	if !errors.Is(err, media.ErrTuning) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unhelpful tuner error: %v", err)
	}
}
