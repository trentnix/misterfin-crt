package jellyfin

import "context"

// RegisterRemoteCapabilities advertises only implemented remote operations.
// Playback-state commands are enabled by SupportsMediaControl. General commands
// are explicit so unsupported volume and screenshot controls are not offered.
func (c *Client) RegisterRemoteCapabilities(ctx context.Context) error {
	_, err := c.request(ctx, "POST", "/Sessions/Capabilities/Full", nil, struct {
		PlayableMediaTypes           []string
		SupportedCommands            []string
		SupportsMediaControl         bool
		SupportsPersistentIdentifier bool
	}{[]string{"Audio", "Video"}, []string{"DisplayMessage", "SetRepeatMode", "SetShuffleQueue", "SetPlaybackOrder"}, true, true})
	return err
}
