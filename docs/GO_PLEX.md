# Experimental Plex support

The first Plex adapter supports account linking, movie, TV, and music libraries, seasons and episodes, Continue Watching, artwork, and playback. It uses the existing browser, controls, picture modes, and output implementations. Jellyfin remains the default.

## Run locally

Add a `server` section to your development `settings.json`. Preserve any other sections:

```json
{
  "server": {
    "provider": "plex",
    "url": "http://your-plex-server:32400"
  }
}
```

From the repository root, run:

```sh
python3 tools/ghostty/ghostty_harness.py --browse --ntsc --inline-video \
  --settings /path/to/settings.json \
  --state-dir /path/to/development-state
```

On first use, open `https://plex.tv/link` in a browser signed in to the account that can access your server. Enter the displayed code. Internet access is required for account linking. Saved sign-in is validated against the configured server on later launches.

The server URL can use HTTP or HTTPS and a reverse-proxy base path. TLS certificates are verified unless `server.insecure_tls` is true. That override never applies to Plex account linking. Credentials, queries, and fragments are not allowed in the URL. Do not put a Plex token in settings. Restart after changing the server section.

Plex credentials live in `plex/session.json` beneath the state directory. Jellyfin keeps its existing `session.json`. Artwork and playback choices use separate provider identities, even if item IDs overlap. Switching to Jellyfin uses the same fields with `provider: "jellyfin"` and its URL. Removing `server` permits legacy `jellyfin.conf` fallback. Unknown providers stop startup instead of silently selecting another server.

## Playback and limits

The adapter requests progressive H.264 video with MP3 audio in Matroska, bounded by `server.transcode` (default 720×576 at 12 Mbps) and 30 fps for NTSC or 25 fps for PAL. Go opens the authenticated stream and passes bytes to the existing decoder. Resume and progress use Plex milliseconds translated to the shared timebase. Each prepared stream owns its timeline reporter and transcode cleanup. Video preparation registers a Plex playback decision before opening the stream. The decision, stream, timeline reports, and cleanup use the same playback identity. A seek creates a new identity so the old stop report cannot terminate its replacement.

Subtitle tracks appear in the existing View menu. Plex burns image and embedded subtitle tracks into the video. Changing those tracks replaces the stream at the current position. Sidecar text subtitles download as UTF-8 SubRip and use the shared overlay without replacing playback. The adapter marks tracks that require burn-in through `MediaStream.RequiresBurnIn`. Jellyfin retains its existing codec-based behavior.

Music uses artist → album → track navigation, ordered album queues, and whole-library shuffle. Original audio files pass through the authenticated loopback proxy with byte-range support. Shared controls handle pause, track changes, and music visuals. Album queues are bounded to 10,000 rows. Shuffle requests return up to 64 tracks per batch.

Validation covered saved sign-in, library and series navigation, Continue Watching, artwork, music queues and decoding, and overlapping video streams during seeking against a local Plex server. Basic MiSTer playback has also been tested. Sustained Plex playback smoothness still needs validation. The shared transcode limits apply to both providers. Jellyfin's MPEG-2 codec selection does not apply to Plex.

Photos, multi-file videos, Plex Home profile switching, server discovery, Live TV, and Plex remote control are not implemented. Photo libraries are omitted. The linked account determines library access. Account avatars and profile-selection UX remain future work.

## Code boundaries

[Application assembly](../cmd/misterfin-crt/server.go) selects a [`connection.Connector`](../internal/connection/connector.go). [`plex.Connector`](../internal/plex/connector.go) links the account and returns a [`media.Server`](../internal/media/server.go) implemented by `plex.Client`. Plex endpoints, response types, and transcode policy stay in [`internal/plex`](../internal/plex). Neither the browser nor the renderer imports the adapter.

[`serverstate`](../internal/serverstate/session.go) supplies private, atomic session storage to both adapters. The existing catalog and playback contracts cover both video and music. Provider-specific additions stay in the adapter.
