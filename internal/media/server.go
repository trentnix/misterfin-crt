package media

import (
	"context"
	"image"
	"io"
	"net/http"
)

// Identity scopes saved choices and cached artwork to one server account.
// Server must distinguish providers. Neither field may contain credentials.
type Identity struct{ Server, User string }

// Artwork fetches decoded images. Returned images are immutable and owned by
// the caller. Missing images return nil without an error.
type Artwork interface {
	ImageKind(context.Context, Item, string) (image.Image, error)
	Photo(context.Context, Item, int, int) (image.Image, error)
}

// Catalog serves paged navigation and the small feeds used by the home screen.
// Implementations must honor cancellation and return owned item slices.
type Catalog interface {
	List(context.Context, Location, int, int) (Page, error)
	Libraries(context.Context) (Page, error)
	Details(context.Context, string) (Item, error)
	LibraryCount(context.Context, Item) (*int, error)
	Mosaic(context.Context, Item) (Page, error)
	ContinueWatching(context.Context) (Page, error)
	RandomTracks(context.Context, string) ([]Item, error)
	AudioQueue(context.Context, Location) ([]Item, error)
}

// StreamSource keeps authenticated requests inside Go. RequestStream supports
// GET/HEAD and byte ranges for the local audio proxy. Callers close response
// bodies. URLs must originate from this server's playback preparation.
type StreamSource interface {
	OpenStream(context.Context, string) (io.ReadCloser, error)
	RequestStream(context.Context, string, string, http.Header) (*http.Response, error)
}

// VideoRequest describes one prepared video stream. The backend translates the
// shared timebase and stream IDs into its own protocol. BurnSubtitle is -1 when
// subtitles are disabled or rendered by the client.
type VideoRequest struct {
	Item         Item
	SessionID    string
	StartTicks   int64
	NTSC         bool
	SourceID     string
	Tracks       TrackSelection
	BurnSubtitle int
}

// Playback translates shared playback choices and reports into server requests.
// Preparation returns owned streams, never player arguments or diagnostic text.
// Implementations may perform network I/O and must honor the supplied context.
// If preparation fails, the adapter must release any resources it allocated.
type Playback interface {
	StreamSource
	Identity() Identity
	PlaybackDetails(context.Context, string) (Item, error)
	PrepareVideo(context.Context, VideoRequest) (PreparedStream, error)
	PrepareAudio(context.Context, Item, string) (PreparedStream, error)
	Subtitle(context.Context, string, string, int) ([]byte, error)
}

// Server is an authenticated backend shared by a browser session. Its methods
// may run concurrently. Authentication and configuration finish before use.
type Server interface {
	Catalog
	Artwork
	Playback
}

// Progress records playback state and resume history. Reporting does not release
// resources. The owner closes the prepared stream even if reporting fails.
type Progress interface {
	ReportPlaying(context.Context, string, PlayState) error
	SavePlaybackPosition(context.Context, string, int64, bool) error
}

// LiveRequest describes one live-edge stream. AudioIndex is an index from a
// preceding preparation's Streams, or -1 to use the server default. Adapters
// must resolve the index against fresh tuner metadata before selecting it.
type LiveRequest struct {
	ChannelID    string
	MaxFrameRate float64
	AudioIndex   int
}

// LiveTV is an optional capability for negotiating tuner streams.
type LiveTV interface {
	PrepareLive(context.Context, LiveRequest) (PreparedStream, error)
}

// RemoteCatalog is an optional capability for resolving remote playback queues.
type RemoteCatalog interface {
	RemoteItems(context.Context, []string, bool) ([]Item, error)
}

// PreparedStream owns server resources for one playback attempt. URL is private
// and must never appear in logs or player arguments. Reports is required. Release
// is optional and must honor its context. The caller invokes it exactly once,
// after stream consumption and final reporting, including on startup failure.
// SessionID and SourceID are opaque identifiers. Streams is immutable.
type PreparedStream struct {
	URL, SessionID, SourceID string
	Streams                  []MediaStream
	LiveAudio                bool // The server can select audio on subsequent live preparations.
	Limits                   StreamLimits
	Reports                  Progress
	Release                  func(context.Context) error
}

// StreamLimits contains numeric transcode limits for diagnostics, not a
// requested decoder profile. Zero means absent or unavailable. Adapters must
// reject negative, non-finite, or unreasonable values from server responses.
type StreamLimits struct {
	MaxWidth, MaxHeight, VideoBitrate, MaxFrameRate float64
}
