package remote

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/coder/websocket"
	"mistervision/internal/jellyfin"
	"mistervision/internal/remote"
)

// Source owns a reconnecting Jellyfin socket. It never calls playback or rendering.
// Construct it after authentication. The borrowed client must outlive Run.
type Source struct {
	client *jellyfin.Client
	retry  time.Duration
}

// New uses the authenticated client's origin, credentials, and TLS policy.
func New(client *jellyfin.Client) *Source { return &Source{client: client, retry: 5 * time.Second} }

// Run registers capabilities on each connection and forwards semantic commands.
// Cancellation interrupts dialing, reads, heartbeat writes, and retry delays.
func (s *Source) Run(ctx context.Context, emit func(remote.Command)) {
	for ctx.Err() == nil {
		s.connect(ctx, emit)
		timer := time.NewTimer(s.retry)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// Publish attaches queue state to normal Jellyfin playback reporting without I/O.
func (s *Source) Publish(state remote.QueueState) {
	q := jellyfin.PlaybackQueue{Current: state.Current, RepeatMode: string(state.Repeat), PlaybackOrder: "Default", Items: make([]jellyfin.QueueItem, len(state.Entries))}
	if state.Shuffled {
		q.PlaybackOrder = "Shuffle"
	}
	for i, e := range state.Entries {
		q.Items[i] = jellyfin.QueueItem{ID: e.ID, PlaylistItemID: e.Key}
		if e.Key == state.Current {
			q.ItemID = e.ID
		}
	}
	s.client.SetPlaybackQueue(q)
}

func (s *Source) connect(ctx context.Context, emit func(remote.Command)) {
	u, err := url.Parse(s.client.Config.Server + "/socket")
	if err != nil {
		return
	}
	query := u.Query()
	query.Set("api_key", s.client.Session.Token)
	query.Set("deviceId", s.client.Session.DeviceID)
	u.RawQuery = query.Encode()
	// Dial accepts HTTP(S), upgrades it, and shares Go's certificate validation.
	client := *s.client.HTTP
	client.Timeout = 0
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	dial, cancel := context.WithTimeout(ctx, 10*time.Second)
	conn, response, err := websocket.Dial(dial, u.String(), &websocket.DialOptions{
		HTTPClient: &client,
		HTTPHeader: http.Header{"Authorization": {s.client.Authorization()}},
	})
	cancel()
	status := 0
	if response != nil {
		status = response.StatusCode
	}
	// Dial errors can contain the credential-bearing URL. Record status only.
	s.client.Diagnostics.Record("remote.socket", slog.Int("status", status), slog.Bool("failed", err != nil))
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)
	work, stop := context.WithCancel(ctx)
	defer stop()
	registration, finish := context.WithTimeout(work, 10*time.Second)
	err = s.client.RegisterRemoteCapabilities(registration)
	finish()
	if err != nil {
		return
	}
	intervals := make(chan time.Duration, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-work.Done():
				return
			case d := <-intervals:
				ticker.Reset(d)
			case <-ticker.C:
				write, cancel := context.WithTimeout(work, 5*time.Second)
				err := conn.Write(write, websocket.MessageText, []byte(`{"MessageType":"KeepAlive"}`))
				if err == nil {
					err = conn.Ping(write)
				}
				cancel()
				if err != nil {
					stop()
					return
				}
			}
		}
	}()
	defer func() { stop(); <-done }()
	for work.Err() == nil {
		kind, data, err := conn.Read(work)
		if err != nil {
			return
		}
		if kind != websocket.MessageText || !json.Valid(data) {
			continue
		}
		if seconds := keepAliveInterval(data); seconds > 0 {
			select {
			case intervals <- time.Duration(seconds) * time.Second:
			default:
			}
		}
		if command, ok := decode(data); ok {
			emit(command)
		}
	}
}

var _ remote.Source = (*Source)(nil)
