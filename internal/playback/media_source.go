package playback

import (
	"context"
	"io"

	"misterfin-go/internal/jellyfin"
)

// mediaSource owns either an authenticated stream or a local audio proxy.
// URLs stay inside this package and never enter diagnostics.
type mediaSource struct {
	stream     io.ReadCloser
	url        string
	closeProxy func()
}

// openMedia authenticates upstream access. Native and helper-based audio use a
// local proxy for range requests. Other playback uses a pipe-fed stream. The
// caller owns the returned source and must close it after the decoder finishes.
func openMedia(ctx context.Context, c *jellyfin.Client, item jellyfin.Item, url string, o Options) (*mediaSource, error) {
	source := &mediaSource{}
	var err error
	if item.Type == "Audio" && (o.TerminalPlayer != "" || !o.Headless) {
		source.url, source.closeProxy, err = audioProxy(ctx, c, url)
	} else {
		source.stream, err = c.OpenStream(ctx, url)
	}
	if err != nil {
		return nil, err
	}
	return source, nil
}

func (s *mediaSource) close() {
	if s.stream != nil {
		_ = s.stream.Close()
	}
	if s.closeProxy != nil {
		s.closeProxy()
	}
}
