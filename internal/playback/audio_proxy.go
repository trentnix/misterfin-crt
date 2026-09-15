package playback

import (
	"context"
	"errors"
	"io"
	"misterfin-crt/internal/jellyfin"
	"net"
	"net/http"
	"time"
)

// audioProxy keeps credentials and TLS in Go while allowing the decoder to seek
// through the original file with HTTP Range requests. Only one opaque local
// path is served. A player cannot select a different upstream URL.
func audioProxy(ctx context.Context, c *jellyfin.Client, upstream string) (string, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, errors.New("cannot open local audio stream")
	}
	nonce, err := jellyfin.NewPlaySessionID()
	if err != nil {
		listener.Close()
		return "", nil, err
	}
	work, cancel := context.WithCancel(ctx)
	transport := c.HTTP.Transport
	if original, ok := transport.(*http.Transport); ok {
		clone := original.Clone()
		clone.ResponseHeaderTimeout = 15 * time.Second
		transport = clone
	}
	client := &http.Client{Transport: transport, CheckRedirect: c.HTTP.CheckRedirect}
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
		if c.Diagnostics != nil {
			started := time.Now()
			defer func() {
				c.Diagnostics.Request(r.Method, "/audio-stream", status, time.Since(started), received, failed)
			}()
		}
		request, err := http.NewRequestWithContext(r.Context(), r.Method, upstream, nil)
		if err != nil {
			w.WriteHeader(502)
			return
		}
		// Preserve byte offsets even if a reverse proxy compresses responses.
		request.Header.Set("Accept-Encoding", "identity")
		for _, name := range []string{"Range", "If-Range"} {
			if value := r.Header.Get(name); value != "" {
				request.Header.Set(name, value)
			}
		}
		response, err := client.Do(request)
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
	closeProxy := func() { cancel(); _ = server.Close(); client.CloseIdleConnections(); <-done }
	return "http://" + listener.Addr().String() + path, closeProxy, nil
}
