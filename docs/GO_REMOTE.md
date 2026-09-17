# Jellyfin remote control

After sign-in, select **MiSTerVision** as the playback device in another Jellyfin client. Remote control works on MiSTer and in the development harness without an extra listening port or settings section.

## Commands and queues

| Command | Behavior |
| --- | --- |
| Play | Start audio/video, including an ordered queue, starting index, and explicit position. |
| Play next / Play last | Insert after the current entry or append. Duplicate items have separate queue identifiers. |
| Previous / Next | Move one queue entry per command without first restarting the track. |
| Pause / Resume / Toggle | Work independently of visible menus. Repeated explicit Pause/Resume is idempotent. |
| Stop | Cancel pending remote playback and return to browsing. |
| Seek | Recorded video replaces its stream. Music uses decoder seeking. Live TV does not seek. |
| Shuffle | Reorder upcoming entries without restarting the current item. Disabling restores original order. |
| Repeat | RepeatNone, RepeatAll, and RepeatOne. Explicit track navigation still changes entries. |
| Message | Display an administrator message for eight seconds through the shared UI. |

Playback reports include queue order, current occurrence, repeat/shuffle state, and seek capability. Reordering around the current item preserves the decoder when no new start position is requested. Local album playback and rolling library-shuffle batches also report their queues. An explicit remote queue operation takes over the retained batch.

Queues are limited to 10,000 entries, with a 30-second lookup limit. Queue and repeat state are not saved across application restarts. Only Audio and Video are advertised. Remote volume, mute, photos, screenshots, subtitles, and audio-track selection are not advertised. Local video options remain available. FFplay lacks music seeking and cannot show shared messages inside its separate video window.

## Connection and recovery

The adapter uses the authenticated Jellyfin WebSocket and the configured HTTP/HTTPS base path. It sends the normal authorization header, verifies TLS unless `server.insecure_tls` is true (or legacy `INSECURE_TLS` applies), and refuses socket redirects. Capabilities register on every connection. Heartbeats check liveness, and disconnected sockets retry after five seconds while browsing stays usable.

A new sign-in cancels the old source and rejects stale account commands. Exit cancels and joins the source. Frames, queued commands, and messages are bounded. [Diagnostics](GO_DIAGNOSTICS.md) records connection status as `remote.socket`, excluding credentials and socket URLs. If no cast target appears, check that sign-in succeeded and registration reached the server.

## Implementation

[`remote.Source`](../internal/remote/source.go) emits owned commands and accepts queue snapshots without network work on the browser loop. [`jellyfin/remote.Source`](../internal/jellyfin/remote/source.go) owns protocol translation, capability registration, and reconnection. [`remote.Queue`](../internal/remote/queue.go) owns occurrence IDs, ordering, shuffle, and repeat. A different control mechanism can implement `remote.Source`.

The browser owns catalog requests and playback transitions. `PlaybackController` owns the decoder handoff. Remote sources never render or call players directly.

`browser.Config.Connector` supplies sign-in and returns a `connection.Session`. Its `Remote` field holds the authenticated source. A nil source disables remote control, as in the Plex adapter. The browser starts and stops the source with that session.

This implements the applicable queue/control behavior requested in [MiSTerFin issue #39](https://github.com/puddingstudio/MiSTerFin/issues/39). Automated tests cover protocol validation, reconnects, cancellation, queue ordering, stale results, and the built-client path. Remote movie and episode playback and controls have also been tested on the CRT.
