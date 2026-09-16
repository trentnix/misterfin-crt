package playback

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"misterfin-crt/internal/diagnostics"
	"misterfin-crt/internal/media"
)

// audioProxy keeps credentials and TLS in Go while allowing the decoder to seek
// through the original file with HTTP Range requests. Only one opaque local
// path is served. A player cannot select a different upstream URL.
func audioProxy(ctx context.Context, c media.StreamSource, upstream string, log *diagnostics.Log) (string, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, errors.New("cannot open local audio stream")
	}
	nonce, err := media.NewPlaySessionID()
	if err != nil {
		listener.Close()
		return "", nil, err
	}
	work, cancel := context.WithCancel(ctx)
	path := "/" + nonce
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 10 * time.Second, BaseContext: func(net.Listener) context.Context { return work }}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path || r.URL.RawQuery != "" {
			http.NotFound(w, r)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		status := 0
		var received int64
		failed := true
		if log != nil {
			started := time.Now()
			defer func() {
				log.Request(r.Method, "/audio-stream", status, time.Since(started), received, failed)
			}()
		}
		response, err := c.RequestStream(r.Context(), r.Method, upstream, r.Header)
		if err != nil {
			w.WriteHeader(502)
			return
		}
		defer response.Body.Close()
		status = response.StatusCode
		failed = status < 200 || status >= 300
		for _, name := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
			if value := response.Header.Get(name); value != "" {
				w.Header().Set(name, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		if r.Method == "GET" {
			var copyErr error
			received, copyErr = io.Copy(w, response.Body)
			failed = failed || copyErr != nil
		}
	})
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	closeProxy := func() { cancel(); _ = server.Close(); <-done }
	return "http://" + listener.Addr().String() + path, closeProxy, nil
}
