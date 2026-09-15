package jellyfin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCollectionQueries(t *testing.T) {
	for _, tc := range []struct{ collection, extra, fields, userData string }{
		{"movies", "Recursive=true&IncludeItemTypes=Movie", "ProductionYear,RunTimeTicks", "true"},
		{"musicvideos", "Recursive=true&IncludeItemTypes=MusicVideo", "ProductionYear,RunTimeTicks", "true"},
		{"music", "", "ProductionYear,RunTimeTicks,ChildCount", "true"},
		{"homevideos", "IncludeItemTypes=Folder,PhotoAlbum,Video,Photo", "ProductionYear,RunTimeTicks", "false"},
		{"mixed", "", "ProductionYear,RunTimeTicks", "false"},
		{"tvshows", "", "ProductionYear,RunTimeTicks,ChildCount,RecursiveItemCount", "true"},
		{"plugin-defined", "", "ProductionYear,RunTimeTicks,ChildCount,RecursiveItemCount", "true"},
	} {
		t.Run(tc.collection, func(t *testing.T) {
			want, _ := url.ParseQuery("userId=user-id&ParentId=view&SortBy=SortName&SortOrder=Ascending&Fields=" + tc.fields + "&EnableUserData=" + tc.userData + "&ImageTypeLimit=1&EnableImageTypes=Primary,Backdrop&StartIndex=12&Limit=34&" + tc.extra)
			got := ItemsQuery("user-id", "view", tc.collection, 12, 34)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v, want %v", got, want)
			}
		})
	}
}

func TestTransportAndPaging(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jellyfin/Items" || r.URL.Query().Get("ParentId") != "a/b?c&d" || r.URL.Query().Get("StartIndex") != "64" {
			t.Errorf("request: %s", r.URL)
		}
		if !strings.Contains(r.Header.Get("Authorization"), `Token="secret"`) {
			t.Error("missing token")
		}
		fmt.Fprint(w, `{"Items":[{"Id":"one","Name":"Don\u0027t Look Up","UserData":{"Played":true}}],"TotalRecordCount":65}`)
	}))
	defer s.Close()
	c := NewClient(Config{Server: s.URL + "/jellyfin"}, Session{Token: "secret"})
	p, err := c.List(context.Background(), Location{ParentID: "a/b?c&d", Collection: "movies"}, 64, 64)
	if err != nil || len(p.Items) != 1 || p.Items[0].Name != "Don't Look Up" || !p.Items[0].UserData.Played || *p.TotalRecordCount != 65 {
		t.Fatalf("page %+v: %v", p, err)
	}
}

func TestMalformedListsAndStatus(t *testing.T) {
	for _, body := range []string{`{}`, `{"Items":null}`, `{"Items":[{"Name":"missing ID"}]}`, `{"Items":[],"TotalRecordCount":-1}`, `not json`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		c := NewClient(Config{Server: s.URL}, Session{})
		if _, err := c.List(context.Background(), Location{Kind: "views"}, 0, 64); err == nil {
			t.Errorf("accepted %s", body)
		}
		s.Close()
	}
	for _, status := range []int{401, 403, 500} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		_, err := NewClient(Config{Server: s.URL}, Session{}).List(context.Background(), Location{}, 0, 64)
		if Rejected(err) != (status != 500) {
			t.Errorf("status %d: %v", status, err)
		}
		s.Close()
	}
}

func TestCancelAndRedirect(t *testing.T) {
	started := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer s.Close()
	c := NewClient(Config{Server: s.URL}, Session{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.List(ctx, Location{}, 0, 64); done <- err }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("request did not cancel")
	}
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("followed foreign redirect") }))
	defer foreign.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, foreign.URL, http.StatusFound) }))
	defer redirect.Close()
	_, err := NewClient(Config{Server: redirect.URL}, Session{Token: "private"}).List(context.Background(), Location{}, 0, 64)
	if err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("redirect error: %v", err)
	}
}

func TestTemporaryFailurePreservesSession(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer s.Close()
	dir := t.TempDir()
	session := Session{Server: s.URL, DeviceID: "go-device", Token: "saved", UserID: "user"}
	if err := SaveSession(dir, session); err != nil {
		t.Fatal(err)
	}
	c := NewClient(Config{Server: s.URL}, session)
	if err := c.Authenticate(context.Background(), dir, func(string) { t.Error("started replacement sign-in") }); err == nil {
		t.Fatal("accepted 503")
	}
	got, _, err := LoadSession(dir, s.URL)
	if err != nil || got != session {
		t.Fatalf("lost session: %+v %v", got, err)
	}
}

func TestQuickConnectReplacesRejectedSession(t *testing.T) {
	seenCode := ""
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/UserViews":
			w.WriteHeader(401)
		case "/QuickConnect/Enabled":
			fmt.Fprint(w, `true`)
		case "/QuickConnect/Initiate":
			if r.Method != "POST" || strings.Contains(r.Header.Get("Authorization"), "Token=") {
				t.Error("bad initiate request")
			}
			fmt.Fprint(w, `{"Secret":"s&?", "Code":"123456"}`)
		case "/QuickConnect/Connect":
			if r.URL.Query().Get("secret") != "s&?" {
				t.Error("secret escaping")
			}
			fmt.Fprint(w, `{"Authenticated":true}`)
		case "/Users/AuthenticateWithQuickConnect":
			var body map[string]string
			if json.NewDecoder(r.Body).Decode(&body) != nil || body["Secret"] != "s&?" {
				t.Error("bad authentication body")
			}
			fmt.Fprint(w, `{"AccessToken":"new-token","User":{"Id":"new-user"}}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	dir := t.TempDir()
	c := NewClient(Config{Server: s.URL}, Session{Server: s.URL, DeviceID: "device", Token: "old", UserID: "user"})
	if err := c.Authenticate(context.Background(), dir, func(code string) { seenCode = code }); err != nil {
		t.Fatal(err)
	}
	saved, _, err := LoadSession(dir, s.URL)
	if err != nil || saved.Token != "new-token" || saved.UserID != "new-user" || seenCode != "123456" {
		t.Fatalf("saved %+v code %q error %v", saved, seenCode, err)
	}
	info, _ := os.Stat(filepath.Join(dir, "session.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("session is not private")
	}
}

func TestConfigAndServerBinding(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jellyfin.conf")
	os.WriteFile(path, []byte("# comment\nhttps://example.test/jellyfin/\nNTSC\n640x480@1000000\nDEBUGLOG\nkey\nname\nINSECURE_TLS\n"), 0600)
	c, err := LoadConfig(path)
	if err != nil || c.Server != "https://example.test/jellyfin" || c.APIKey != "key" || c.Username != "name" || c.TVMode != "NTSC" || !c.InsecureTLS {
		t.Fatalf("%+v %v", c, err)
	}
	SaveSession(dir, Session{Server: "http://old", DeviceID: "old-device", Token: "old-secret", UserID: "old-user"})
	s, _, err := LoadSession(dir, "http://new")
	if err != nil || s.Token != "" || s.UserID != "" || s.DeviceID == "old-device" {
		t.Fatalf("cross-server session reused: %+v %v", s, err)
	}
	for _, raw := range []string{"ftp://example.test", "http://user:pass@example.test", "http://example.test?secret=x"} {
		os.WriteFile(path, []byte(raw), 0600)
		if _, err := LoadConfig(path); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestDetailsAndMosaicRequests(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("userId") != "viewer" {
			t.Error("missing user context")
		}
		switch r.URL.Path {
		case "/Items/movie":
			if q.Get("EnableUserData") != "true" || q.Get("EnableImageTypes") != "Primary,Logo,Backdrop" || !strings.Contains(q.Get("Fields"), "Overview") {
				t.Errorf("details query: %v", q)
			}
			fmt.Fprint(w, `{"Id":"movie","Overview":"Description","CommunityRating":8.2,"BackdropImageTags":["backdrop"]}`)
		case "/Items":
			if q.Get("IncludeItemTypes") != "MusicAlbum" || q.Get("Recursive") != "true" || q.Get("Limit") != "12" || q.Get("ParentId") != "music" {
				t.Errorf("mosaic query: %v", q)
			}
			fmt.Fprint(w, `{"Items":[],"TotalRecordCount":120}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer s.Close()
	c := NewClient(Config{Server: s.URL}, Session{UserID: "viewer"})
	item, err := c.Details(context.Background(), "movie")
	if err != nil || item.Overview != "Description" || item.CommunityRating != 8.2 {
		t.Fatalf("details: %+v %v", item, err)
	}
	page, err := c.Mosaic(context.Background(), Item{ID: "music", CollectionType: "music"})
	if err != nil || page.TotalRecordCount == nil || *page.TotalRecordCount != 120 {
		t.Fatalf("mosaic: %+v %v", page, err)
	}
}
