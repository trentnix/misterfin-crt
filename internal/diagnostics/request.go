package diagnostics

import (
	"log/slog"
	"net/url"
	"time"
)

// Request records HTTP metadata without accepting headers or bodies. Query
// strings, fragments, origins, and user information are stripped defensively.
// bytes is the buffered response length, or zero for a streaming header event.
// A stream's duration measures opening through headers, not total playback.
func (l *Log) Request(method, path string, status int, elapsed time.Duration, bytes int64, failed bool) {
	if l == nil {
		return
	}
	u, err := url.Parse(path)
	if err != nil {
		path = "invalid-path"
	} else {
		path = u.EscapedPath()
	}
	if len(path) > 256 {
		path = path[:256]
	}
	switch method {
	case "GET", "POST", "HEAD", "DELETE":
	default:
		method = "OTHER"
	}
	l.Record("http.request", slog.String("method", method), slog.String("path", path),
		slog.Int("status", status), slog.Int64("elapsed_ms", elapsed.Milliseconds()),
		slog.Int64("bytes", bytes), slog.Bool("failed", failed))
}
