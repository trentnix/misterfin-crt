package jellyfin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise the shared safeguards through both consumer-facing stream operations.
func TestStreamsRejectInvalidDestinations(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer server.Close()
	client := NewClient(Config{Server: server.URL}, Session{})
	for _, raw := range []string{
		"http://other.invalid/stream?ApiKey=private",
		"/relative?ApiKey=private",
		strings.Replace(server.URL, "http://", "http://private@", 1) + "/stream",
		server.URL + "/stream#private",
		"ftp" + strings.TrimPrefix(server.URL, "http") + "/private",
		"://private",
	} {
		if body, err := client.OpenStream(t.Context(), raw); err == nil {
			body.Close()
			t.Fatal("video accepted invalid destination")
		} else if strings.Contains(err.Error(), "private") {
			t.Fatal("video error leaked URL")
		}
		if response, err := client.RequestStream(t.Context(), http.MethodGet, raw, nil); err == nil {
			response.Body.Close()
			t.Fatal("audio accepted invalid destination")
		} else if strings.Contains(err.Error(), "private") {
			t.Fatal("audio error leaked URL")
		}
	}
	if _, err := client.RequestStream(t.Context(), http.MethodPost, server.URL, nil); err == nil {
		t.Fatal("accepted unsupported stream method")
	}
	if hits.Load() != 0 {
		t.Fatal("invalid requests reached server")
	}
}

func TestStreamRedirectsStayOnServer(t *testing.T) {
	var foreignHits atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { foreignHits.Add(1) }))
	defer foreign.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/local":
			http.Redirect(w, r, "/stream", http.StatusFound)
		case "/foreign":
			http.Redirect(w, r, foreign.URL+"/stream?ApiKey=private", http.StatusFound)
		default:
			_, _ = io.WriteString(w, "media")
		}
	}))
	defer server.Close()
	client := NewClient(Config{Server: server.URL}, Session{})
	defer client.HTTP.CloseIdleConnections()
	for _, audio := range []bool{false, true} {
		for _, path := range []string{"/local", "/foreign"} {
			var body io.ReadCloser
			var err error
			if audio {
				var response *http.Response
				response, err = client.RequestStream(t.Context(), http.MethodGet, server.URL+path+"?ApiKey=private", nil)
				if err == nil {
					body = response.Body
				}
			} else {
				body, err = client.OpenStream(t.Context(), server.URL+path+"?ApiKey=private")
			}
			if path == "/foreign" {
				if err == nil {
					body.Close()
					t.Fatal("accepted cross-origin redirect")
				}
				if strings.Contains(err.Error(), "private") {
					t.Fatal("redirect error leaked credentials")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(body)
				body.Close()
				if err != nil || string(data) != "media" {
					t.Fatal("same-origin redirect lost stream")
				}
			}
		}
	}
	if foreignHits.Load() != 0 {
		t.Fatal("followed foreign redirect")
	}
}

func TestStreamStatusHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes */5")
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
	}))
	defer server.Close()
	client := NewClient(Config{Server: server.URL}, Session{})
	defer client.HTTP.CloseIdleConnections()
	_, err := client.OpenStream(t.Context(), server.URL)
	var status *HTTPError
	if !errors.As(err, &status) || status.Status != 416 {
		t.Fatal("video did not reject HTTP failure")
	}
	response, err := client.RequestStream(t.Context(), http.MethodGet, server.URL, http.Header{"Range": {"bytes=10-"}})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 416 || response.Header.Get("Content-Range") != "bytes */5" {
		t.Fatal("audio lost range response")
	}
}

func TestStreamBodiesOutliveJSONTimeout(t *testing.T) {
	for _, audio := range []bool{false, true} {
		name := "video"
		if audio {
			name = "audio"
		}
		t.Run(name, func(t *testing.T) {
			closeRequest := make(chan bool, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				closeRequest <- r.Close
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				time.Sleep(30 * time.Millisecond)
				_, _ = io.WriteString(w, "media")
			}))
			defer server.Close()
			client := NewClient(Config{Server: server.URL}, Session{})
			defer client.HTTP.CloseIdleConnections()
			client.HTTP.Timeout = time.Millisecond
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			var body io.ReadCloser
			var err error
			if audio {
				var response *http.Response
				response, err = client.RequestStream(ctx, http.MethodGet, server.URL, nil)
				if err == nil {
					body = response.Body
				}
			} else {
				body, err = client.OpenStream(ctx, server.URL)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer body.Close()
			data, err := io.ReadAll(body)
			if err != nil || string(data) != "media" {
				t.Fatal("media inherited JSON timeout", err)
			}
			if got := <-closeRequest; got == audio {
				t.Fatal("changed video/audio connection policy")
			}
		})
	}
}
