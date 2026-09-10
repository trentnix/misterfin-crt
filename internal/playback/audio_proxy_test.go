package playback

import (
	"bytes"
	"context"
	"io"
	"misterfin-go/internal/jellyfin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAudioProxyRangesAndPrivacy(t *testing.T) {
	data := []byte("0123456789abcdef")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "identity" {
			t.Error("audio byte offsets could change through compression")
		}
		if r.URL.Query().Get("ApiKey") != "private-token" {
			t.Error("missing upstream credentials")
		}
		http.ServeContent(w, r, "audio", time.Time{}, bytes.NewReader(data))
	}))
	defer server.Close()
	c := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{Token: "private-token"})
	source, closeProxy, err := audioProxy(context.Background(), c, c.AudioStreamURL("track", "session"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeProxy()
	if strings.Contains(source, "private-token") || strings.Contains(source, "track") {
		t.Fatal("private information in player URL")
	}
	req, _ := http.NewRequest("GET", source, nil)
	req.Header.Set("Range", "bytes=5-8")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 206 || string(body) != "5678" || response.Header.Get("Content-Range") != "bytes 5-8/16" {
		t.Fatal("range not preserved")
	}
	req, _ = http.NewRequest("HEAD", source, nil)
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.ContentLength != 16 {
		t.Fatal("missing seekable length")
	}
	response, err = http.Get(source + "?url=http://other")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatal("proxy accepted arbitrary target")
	}
}
