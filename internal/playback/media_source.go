package playback

import (
	"context"
	"io"

	"mistervision/internal/diagnostics"
	"mistervision/internal/media"
	playerapi "mistervision/internal/player"
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
func openMedia(ctx context.Context, c media.StreamSource, url string, input playerapi.Input, log *diagnostics.Log) (*mediaSource, error) {
	source := &mediaSource{}
	var err error
	if input == playerapi.URL {
		source.url, source.closeProxy, err = audioProxy(ctx, c, url, log)
	} else {
		source.stream, err = c.OpenStream(ctx, url)
	}
	if err != nil {
		return nil, err
	}
	return source, nil
}

// close releases the upstream stream or local proxy after decoder cleanup.
func (s *mediaSource) close() {
	if s.stream != nil {
		_ = s.stream.Close()
	}
	if s.closeProxy != nil {
		s.closeProxy()
	}
}
