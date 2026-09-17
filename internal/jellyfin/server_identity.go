package jellyfin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

// VerifyServer checks public server identity without sending authentication or
// device headers. Discovery IDs are hints, so moved addresses must pass this
// check before receiving saved credentials. Redirects are refused and work is bounded.
func VerifyServer(ctx context.Context, address, id string) error {
	if id == "" {
		return errors.New("missing Jellyfin server identity")
	}
	client := NewClient(Config{Server: address}, Session{})
	client.HTTP.Timeout = 5 * time.Second
	client.HTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	defer client.HTTP.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address+"/System/Info/Public", nil)
	if err != nil {
		return errors.New("invalid Jellyfin server address")
	}
	response, err := client.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrServerUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return &HTTPError{Status: response.StatusCode}
	}
	const limit = 64 << 10
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(data) > limit {
		return errors.New("cannot read Jellyfin server identity")
	}
	var info struct {
		ID string `json:"Id"`
	}
	if json.Unmarshal(data, &info) != nil || info.ID != id {
		return errors.New("Jellyfin server identity does not match")
	}
	return nil
}
