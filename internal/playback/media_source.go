package playback

import (
	"context"
	"io"

	"misterfin-crt/internal/jellyfin"
)

// mediaSource owns either an authenticated stream or a local audio proxy.
// URLs stay inside this package and never enter diagnostics.
type mediaSource struct {
	stream     io.ReadCloser
	url        string
	closeProxy func()
}

// openMedia authenticates upstream access through the requested transport. URL
// input uses a local proxy for range requests. Pipe input opens one stream. The
// caller must close the source after the decoder finishes.
func openMedia(ctx context.Context, c *jellyfin.Client, url string, input decoderInput) (*mediaSource, error) {
	source := &mediaSource{}
	var err error
	if input == inputURL {
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
