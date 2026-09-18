package plex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mistervision/internal/media"
	"mistervision/internal/serverstate"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "private-token" || r.Header.Get("X-Plex-Client-Identifier") != "device" {
			t.Error("missing authenticated identity")
		}
		if r.URL.Query().Has("X-Plex-Token") {
			t.Error("token leaked into request URL")
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return NewClient(Config{Server: server.URL}, serverstate.Session{Server: server.URL, DeviceID: "device", Token: "private-token", UserID: "7"})
}

func TestLibraryHierarchyAndPagination(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/livetv/dvrs":
			fmt.Fprint(w, `{"MediaContainer":{"Dvr":[]}}`)
		case "/playlists", "/library/all":
			fmt.Fprint(w, `{"MediaContainer":{"size":0,"totalSize":0}}`)
		case "/library/sections":
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","title":"Nostalgia","type":"movie"},{"key":"2","title":"Series","type":"show"}]}}`)
		case "/library/sections/1/all":
			if r.URL.Query().Get("X-Plex-Container-Start") != "64" || r.URL.Query().Get("X-Plex-Container-Size") != "64" {
				t.Error("incorrect pagination")
			}
			fmt.Fprint(w, `{"MediaContainer":{"offset":64,"totalSize":200,"Metadata":[{"ratingKey":"9","type":"movie","title":"Film"}]}}`)
		case "/library/metadata/20/children":
			fmt.Fprint(w, `{"MediaContainer":{"totalSize":1,"Directory":[{"key":"/library/metadata/20/allLeaves","title":"All episodes"}],"Metadata":[{"ratingKey":"21","parentRatingKey":"20","type":"season","title":"Season 1","index":1}]}}`)
		case "/library/metadata/21/children":
			fmt.Fprint(w, `{"MediaContainer":{"totalSize":1,"Metadata":[{"ratingKey":"22","grandparentRatingKey":"20","type":"episode","title":"Episode","grandparentTitle":"Show","parentIndex":1,"index":2}]}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	libraries, err := c.Libraries(t.Context())
	if err != nil || len(libraries.Items) != 2 || libraries.Items[0].Name != "Nostalgia" || libraries.Items[0].ID != "library:1" {
		t.Fatalf("libraries: %+v %v", libraries, err)
	}
	page, err := c.List(t.Context(), media.Location{Kind: "items", ParentID: "library:1"}, 64, 64)
	if err != nil || *page.TotalRecordCount != 200 || page.Items[0].Type != "Movie" {
		t.Fatalf("movies: %+v %v", page, err)
	}
	seasons, err := c.List(t.Context(), media.Location{Kind: "seasons", SeriesID: "20"}, 0, 64)
	if err != nil || len(seasons.Items) != 1 || seasons.Items[0].SeriesID != "20" {
		t.Fatalf("seasons: %+v %v", seasons, err)
	}
	episodes, err := c.List(t.Context(), media.Location{Kind: "episodes", ParentID: "21", SeriesID: "20"}, 0, 64)
	if err != nil || episodes.Items[0].SeriesName != "Show" || *episodes.Items[0].IndexNumber != 2 {
		t.Fatalf("episodes: %+v %v", episodes, err)
	}
}

func TestMetadataTimebaseAndVideoScope(t *testing.T) {
	var m metadata
	err := json.Unmarshal([]byte(`{"ratingKey":"42","type":"movie","duration":90000,"viewOffset":12500,"viewCount":1,"lastViewedAt":100,"Media":[{"id":4,"width":640,"height":480,"aspectRatio":1.3333,"Part":[{"id":8,"Stream":[{"id":10,"streamType":1,"codec":"h264"},{"id":11,"streamType":2,"codec":"aac","language":"English"},{"id":12,"streamType":3,"codec":"srt"},{"id":13,"streamType":3,"codec":"srt","key":"/library/streams/13"}]}]}]}`), &m)
	if err != nil {
		t.Fatal(err)
	}
	item := m.item()
	if item.RunTimeTicks != 900000000 || item.UserData.PlaybackPositionTicks != 125000000 || item.UserData.Played {
		t.Fatalf("timebase: %+v", item)
	}
	if item.MediaSources[0].ID != "0:8" || item.MediaStreams[0].Width != 640 {
		t.Fatal("source identity or video geometry lost")
	}
	if len(item.MediaStreams) != 4 || !item.MediaStreams[2].RequiresBurnIn || !item.MediaStreams[3].ClientSubtitle() {
		t.Fatal("subtitle delivery constraints lost")
	}

}

func TestTranscodeAndPlaybackReporting(t *testing.T) {
	var selection, urlQuery, timeline url.Values
	stopped, watched := false, false
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/library/parts/8":
			if r.Method != "PUT" {
				t.Error("selection method")
			}
			selection = r.URL.Query()
		case "/video/:/transcode/universal/decision":
			fmt.Fprint(w, `{"MediaContainer":{"generalDecisionCode":1001}}`)
		case "/video/:/transcode/universal/start.mkv":
			urlQuery = r.URL.Query()
			fmt.Fprint(w, "stream")
		case "/:/timeline":
			if r.Method != "POST" {
				t.Error("timeline method")
			}
			timeline = r.URL.Query()
		case "/video/:/transcode/universal/stop":
			stopped = r.URL.Query().Get("session") == "play-session"
		case "/:/scrobble":
			watched = r.Method == "PUT" && r.URL.Query().Get("key") == "42"
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	raw, err := c.PrepareVideo(t.Context(), media.VideoRequest{Item: media.Item{ID: "42", RunTimeTicks: 900000000, MediaSources: []media.MediaSource{{ID: "0:8"}}}, SessionID: "play-session", StartTicks: 125000000, NTSC: true, SourceID: "0:8", Tracks: media.TrackSelection{AudioIndex: 11, SubtitleIndex: -1}, BurnSubtitle: -1})
	if err != nil {
		t.Fatal(err)
	}
	body, err := c.OpenStream(t.Context(), raw.URL)
	if err != nil {
		t.Fatal(err)
	}
	body.Close()
	if selection.Get("audioStreamID") != "11" || selection.Get("subtitleStreamID") != "0" {
		t.Fatal("track selection lost")
	}
	profile := urlQuery.Get("X-Plex-Client-Profile-Extra")
	if urlQuery.Get("offset") != "12.5000000" || urlQuery.Get("protocol") != "http" || urlQuery.Get("directPlay") != "0" || !strings.Contains(profile, "videoCodec=h264") || !strings.Contains(profile, "name=video.frameRate&value=30") {
		t.Fatalf("wrong CRT stream request: %v", urlQuery)
	}
	state := media.PlayState{ItemID: "42", PlaySessionID: "play-session", PositionTicks: 125000000, IsPaused: true}
	if err := raw.Reports.ReportPlaying(t.Context(), "progress", state); err != nil {
		t.Fatal(err)
	}
	if timeline.Get("time") != "12500" || timeline.Get("duration") != "90000" || timeline.Get("state") != "paused" {
		t.Fatal("invalid timeline translation")
	}
	if err := raw.Reports.ReportPlaying(t.Context(), "stopped", state); err != nil {
		t.Fatal(err)
	}
	if stopped {
		t.Fatal("reporting released stream resources")
	}
	if err := raw.Release(t.Context()); err != nil || !stopped {
		t.Fatal("transcode was not stopped")
	}
	if err := raw.Reports.SavePlaybackPosition(t.Context(), "42", 900000000, true); err != nil || !watched {
		t.Fatal("completion was not recorded")
	}
}

func TestRequestsRejectCredentialLeaks(t *testing.T) {
	leaked := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer other.Close()
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) })
	_, err := c.OpenStream(t.Context(), other.URL+"/stream")
	if err == nil {
		t.Fatal("cross-origin input accepted")
	}
	_, err = c.OpenStream(t.Context(), c.Config.Server+"/redirect")
	if err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("redirect error: %v", err)
	}
	if leaked {
		t.Fatal("followed cross-origin redirect")
	}
	_, err = c.ImageKind(t.Context(), media.Item{ImageTags: map[string]string{"Primary": other.URL + "/image"}}, "Primary")
	if err == nil {
		t.Fatal("external artwork URL accepted")
	}
}

func TestArtworkIsBoundedAndRangesSurvive(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/photo/:/transcode" {
			png.Encode(w, image.NewRGBA(image.Rect(0, 0, 800, 600)))
			return
		}
		if r.Header.Get("Range") != "bytes=10-20" || r.Header.Get("Accept-Encoding") != "identity" {
			t.Error("range request changed")
		}
		w.Header().Set("Content-Range", "bytes 10-20/100")
		w.WriteHeader(206)
	})
	im, err := c.ImageKind(t.Context(), media.Item{ImageTags: map[string]string{"Primary": "/library/metadata/4/thumb"}}, "Primary")
	if err != nil || im.Bounds().Dx() > 320 || im.Bounds().Dy() > 360 {
		t.Fatal("artwork exceeded cache bounds")
	}
	response, err := c.RequestStream(t.Context(), "GET", c.Config.Server+"/file", http.Header{"Range": {"bytes=10-20"}})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 206 || response.Header.Get("Content-Range") == "" {
		t.Fatal("range response lost")
	}
}

func TestLinkSignInAndSavedSession(t *testing.T) {
	pinRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/pins":
			pinRequests++
			if r.Header.Get("X-Plex-Token") != "" {
				t.Error("old credential sent to PIN endpoint")
			}
			fmt.Fprint(w, `{"id":2,"code":"ABCD","expiresIn":30}`)
		case "/api/v2/pins/2":
			fmt.Fprint(w, `{"id":2,"code":"ABCD","expiresIn":30,"authToken":"approved-token"}`)
		case "/playlists", "/library/all":
			fmt.Fprint(w, `{"MediaContainer":{"size":0,"totalSize":0}}`)
		case "/library/sections":
			if r.Header.Get("X-Plex-Token") != "approved-token" {
				w.WriteHeader(401)
				return
			}
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[]}}`)
		case "/api/v2/user":
			fmt.Fprint(w, `{"id":7}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	c := NewClient(Config{Server: server.URL}, serverstate.Session{Server: server.URL, DeviceID: "device"})
	c.accountURL = server.URL
	code := ""
	if err := c.Authenticate(t.Context(), dir, func(s string) { code = s }); err != nil {
		t.Fatal(err)
	}
	if code != "ABCD" || c.Session.UserID != "7" {
		t.Fatal("approval result lost")
	}
	saved, _, err := serverstate.LoadSession(dir, server.URL)
	if err != nil || saved.Token != "approved-token" {
		t.Fatal("session not saved")
	}
	info, err := os.Stat(filepath.Join(dir, "session.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("token file must be private")
	}
	if err = c.Authenticate(t.Context(), dir, nil); err != nil || pinRequests != 1 {
		t.Fatal("valid saved sign-in requested approval again")
	}
}

func TestAuthenticationFailurePreservesSavedToken(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) })
			dir := t.TempDir()
			if err := serverstate.SaveSession(dir, c.Session); err != nil {
				t.Fatal(err)
			}
			err := c.Authenticate(t.Context(), dir, nil)
			if err == nil || strings.Contains(err.Error(), "private-token") {
				t.Fatalf("unsafe failure: %v", err)
			}
			saved, _, _ := serverstate.LoadSession(dir, c.Config.Server)
			if saved.Token != "private-token" {
				t.Fatal("failed authentication discarded saved credentials")
			}
		})
	}
}

func TestAuthenticationCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"id":2,"code":"ABCD","expiresIn":30}`) }))
	defer server.Close()
	c := NewClient(Config{Server: server.URL}, serverstate.Session{DeviceID: "device"})
	c.accountURL = server.URL
	ctx, cancel := context.WithCancel(t.Context())
	start := time.Now()
	err := c.Authenticate(ctx, t.TempDir(), func(string) { cancel() })
	if !errors.Is(err, context.Canceled) || time.Since(start) > time.Second {
		t.Fatalf("cancellation did not stop sign-in: %v", err)
	}
}

func TestMetadataDistinguishesRatingRecords(t *testing.T) {
	for _, ratings := range []string{
		`"rating":7.8,"Rating":[{"image":"imdb://image.rating","value":8.0}]`,
		`"Rating":[{"image":"imdb://image.rating","value":8.0}],"rating":7.8`,
	} {
		t.Run(ratings, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/library/metadata/42", "/hubs/continueWatching/items":
					fmt.Fprintf(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"42","type":"movie","title":"Film","duration":90000,"viewOffset":12500,%s}]}}`, ratings)
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			})
			item, err := c.PlaybackDetails(t.Context(), "42")
			if err != nil {
				t.Fatal(err)
			}
			if item.CommunityRating != 7.8 || item.UserData.PlaybackPositionTicks != 125000000 {
				t.Fatalf("rating or resume changed: %+v", item)
			}
			page, err := c.ContinueWatching(t.Context())
			if err != nil || len(page.Items) != 1 || page.Items[0].ContinueAction != "resume" {
				t.Fatalf("continue: %+v, %v", page, err)
			}
		})
	}
}

func TestRejectedSignInRetainsSavedRecordUntilApproval(t *testing.T) {
	var pinRequested bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/playlists", "/library/all":
			fmt.Fprint(w, `{"MediaContainer":{"size":0,"totalSize":0}}`)
		case "/library/sections":
			w.WriteHeader(http.StatusUnauthorized)
		case "/api/v2/pins":
			pinRequested = true
			if r.Header.Get("X-Plex-Token") != "" {
				t.Error("rejected token sent to approval endpoint")
			}
			fmt.Fprint(w, `{"id":2,"code":"ABCD","expiresIn":30}`)
		default:
			t.Error("unexpected request")
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	saved := serverstate.Session{Server: server.URL, DeviceID: "device", Token: "rejected-private", UserID: "7"}
	if err := serverstate.SaveSession(dir, saved); err != nil {
		t.Fatal(err)
	}
	c := NewClient(Config{Server: server.URL}, saved)
	c.accountURL = server.URL
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err := c.Authenticate(ctx, dir, func(string) { cancel() })
	if !errors.Is(err, context.Canceled) || !pinRequested {
		t.Fatalf("relink: %v", err)
	}
	after, _, err := serverstate.LoadSession(dir, server.URL)
	if err != nil || after != saved {
		t.Fatal("canceled approval replaced credentials")
	}
}

func TestStreamBodyOutlivesMetadataTimeoutAndCancels(t *testing.T) {
	canceled := make(chan struct{})
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "frame")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(canceled)
	})
	c.HTTP.Timeout = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	body, err := c.OpenStream(ctx, c.Config.Server+"/video")
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	got := make([]byte, 5)
	if _, err := io.ReadFull(body, got); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := body.Read(make([]byte, 1)); result <- err }()
	select {
	case err := <-result:
		t.Fatalf("stream body inherited metadata timeout: %v", err)
	case <-time.After(40 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stream ignored cancellation")
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("server stream leaked")
	}
}

func TestUnsupportedSourcesFailBeforeRequests(t *testing.T) {
	c := testClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unsupported source made a request") })
	var m metadata
	if err := json.Unmarshal([]byte(`{"ratingKey":"42","type":"movie","Media":[{"Part":[{"id":1},{"id":2}]}]}`), &m); err != nil {
		t.Fatal(err)
	}
	item := m.item()
	if len(item.MediaSources) != 0 {
		t.Fatal("multipart source advertised")
	}
	if _, err := c.PrepareVideo(t.Context(), media.VideoRequest{Item: item, SourceID: "0:1", SessionID: "test", BurnSubtitle: -1}); err == nil {
		t.Fatal("unsupported source accepted")
	}
}

func TestLibraryAndContinueScope(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/playlists", "/library/all":
			fmt.Fprint(w, `{"MediaContainer":{"size":0,"totalSize":0}}`)
		case "/library/sections":
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","title":"Movies","type":"movie"},{"key":"2","title":"Music","type":"artist"},{"key":"3","title":"Photos","type":"photo"}]}}`)
		case "/hubs/continueWatching/items":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"1","type":"movie","viewOffset":5000},{"ratingKey":"2","type":"episode"},{"ratingKey":"3","type":"track"}]}}`)
		}
	})
	libraries, err := c.Libraries(t.Context())
	if err != nil || len(libraries.Items) != 3 || libraries.Items[1].CollectionType != "music" || libraries.Items[2].CollectionType != "photos" {
		t.Fatalf("libraries: %+v %v", libraries, err)
	}
	page, err := c.ContinueWatching(t.Context())
	if err != nil || len(page.Items) != 2 || *page.TotalRecordCount != 2 || page.Items[0].ContinueAction != "resume" || page.Items[1].ContinueAction != "next" {
		t.Fatalf("continue: %+v %v", page, err)
	}
}

func TestMalformedResponsesAndErrorsStayPrivate(t *testing.T) {
	for _, body := range []string{`private-token`, `{}`, `{"MediaContainer":{"totalSize":-1}}`, `{"MediaContainer":{"Metadata":[{"ratingKey":"../private-token"}]}}`} {
		t.Run(body, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
			_, err := c.List(t.Context(), media.Location{ParentID: "library:1"}, 0, 10)
			if err == nil || strings.Contains(err.Error(), "private-token") {
				t.Fatalf("unsafe metadata error: %v", err)
			}
		})
	}
	for _, status := range []int{401, 403, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status); fmt.Fprint(w, "private-token") })
			_, err := c.Details(t.Context(), "1")
			if err == nil || strings.Contains(err.Error(), "private-token") || errors.Is(err, media.ErrUnauthorized) != (status == 401 || status == 403) {
				t.Fatalf("status translation: %v", err)
			}
		})
	}
}

func TestTLSOverrideAppliesOnlyToConfiguredMediaServer(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"MediaContainer":{"Directory":[]}}`) }))
	defer server.Close()
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	saved := serverstate.Session{DeviceID: "device", Token: "private-token", UserID: "7"}
	secure := NewClient(Config{Server: server.URL}, saved)
	if _, err := secure.Libraries(t.Context()); err == nil {
		t.Fatal("default accepted untrusted certificate")
	}
	overridden := NewClient(Config{Server: server.URL, InsecureTLS: true}, saved)
	if _, err := overridden.Libraries(t.Context()); err != nil {
		t.Fatal("explicit server TLS override failed", err)
	}
	if _, _, err := overridden.fetch(t.Context(), overridden.accountHTTP, server.URL, "", "POST", "/api/v2/pins", nil); err == nil {
		t.Fatal("media TLS override weakened account linking")
	}
}

func TestConfiguredTranscodeLimitsReachPlex(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/video/:/transcode/universal/decision" {
			fmt.Fprint(w, `{"MediaContainer":{"generalDecisionCode":1001}}`)
		}
		if r.URL.Path == "/video/:/transcode/universal/start.mkv" {
			q := r.URL.Query()
			if q.Get("videoResolution") != "640x480" || q.Get("videoBitrate") != "8000" || q.Get("maxVideoBitrate") != "8000" {
				t.Error("configured limits not sent to Plex")
			}
			fmt.Fprint(w, "stream")
		}
	})
	c.Config.MaxWidth, c.Config.MaxHeight, c.Config.VideoBitrate = 640, 480, 8000000
	stream, err := c.PrepareVideo(t.Context(), media.VideoRequest{Item: media.Item{ID: "42", MediaSources: []media.MediaSource{{ID: "0:8"}}}, SessionID: "test", SourceID: "0:8", Tracks: media.TrackSelection{AudioIndex: -1, SubtitleIndex: -1}, BurnSubtitle: -1})
	if err != nil {
		t.Fatal(err)
	}
	body, err := c.OpenStream(t.Context(), stream.URL)
	if err != nil {
		t.Fatal(err)
	}
	body.Close()
	if stream.Limits.MaxWidth != 640 || stream.Limits.MaxHeight != 480 || stream.Limits.VideoBitrate != 8000000 {
		t.Fatal("diagnostic limits differ from requested limits")
	}
}

// timeoutTransport exercises the actual adapter boundary without slow clocks.
type timeoutTransport struct{}

func (timeoutTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, os.ErrDeadlineExceeded
}

func TestTimeoutsRemainClassifiableWithoutURLs(t *testing.T) {
	c := NewClient(Config{Server: "https://private-server"}, serverstate.Session{})
	c.HTTP.Transport = timeoutTransport{}
	_, err := c.OpenStream(t.Context(), c.Config.Server+"/stream?private-token=secret")
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, media.ErrUnavailable) || strings.Contains(err.Error(), "private") {
		t.Fatalf("stream timeout: %v", err)
	}
	_, err = c.Libraries(t.Context())
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, media.ErrUnavailable) || strings.Contains(err.Error(), "private") {
		t.Fatalf("metadata timeout: %v", err)
	}
}
