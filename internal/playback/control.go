package playback

// Control requests an action on the active decoder. Unknown kinds are ignored.
type Control struct {
	// Kind is "pause", "refresh", "subtitle", "picture", or "seek" (relative audio seek).
	Kind    string
	Request int         // Correlates subtitle or picture replies with the latest selection.
	Index   int         // Jellyfin stream index for a subtitle request.
	Seconds int         // Signed offset for seek. Video seeks use stream replacement.
	Picture PictureMode // Requested live picture mode.
}
