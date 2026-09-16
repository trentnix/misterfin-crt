package plex

import (
	"context"
	"errors"
	"net/url"
)

// selectLiveAudio maps a broadcast stream index to the current Plex stream ID.
// IDs belong to the tuned session and must not survive a replacement. Return
// true only when every audio choice can be selected through the current part.
// The PUT changes Plex's per-user selection before conversion starts. It does
// not send decoder commands or change audio in an already-open conversion.
func (c *Client) selectLiveAudio(ctx context.Context, video liveVideo, index int, identity url.Values) (bool, error) {
	parts := video.Media[0].Part
	choices := make(map[int]string)
	partID := ""
	if len(parts) == 1 && validID(string(parts[0].ID)) {
		partID = string(parts[0].ID)
		for _, stream := range parts[0].Streams {
			if stream.Type != 2 {
				continue
			}
			if !validID(string(stream.ID)) || stream.Index < 0 || choices[stream.Index] != "" {
				choices = nil
				break
			}
			choices[stream.Index] = string(stream.ID)
		}
	}
	available := len(choices) > 1
	if index == -1 {
		return available, nil
	}
	if !available || choices[index] == "" {
		return false, errors.New("live audio track is no longer available")
	}
	identity.Set("audioStreamID", choices[index])
	_, err := c.request(ctx, "PUT", "/library/parts/"+partID, identity)
	return available, err
}
