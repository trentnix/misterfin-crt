# Plex support

MiSTerVision’s Plex adapter supports account linking, movie, TV, music, and photo libraries, seasons and episodes, Continue Watching, artwork, playback, and tuner-backed Live TV. It shares the browser, controls, picture modes, and output implementations with Jellyfin. Jellyfin is the default provider.

## Server discovery

Open **About → Connections → Plex**. Enter the displayed code at [plex.tv/link](https://plex.tv/link) in a browser signed in to your Plex account. The picker shows the account name and reachable servers. Select a server to connect. No server address is required in configuration.

Discovery combines the servers listed by your account with a short GDM scan of the local IPv4 network. LAN replies only add addresses for servers available to the linked account. Direct local connections take priority, with HTTPS preferred within local and remote groups. Each group probes its addresses concurrently for up to two seconds, so unreachable container interfaces do not prevent fallback to another group.

Each candidate must report the expected server identity before receiving its server token. Discovery respects servers that require HTTPS and verifies TLS certificates. Relay connections are excluded. GDM uses the responding device’s address, so a reachable LAN reply can replace an unreachable container address advertised by the account. GDM currently adds HTTP addresses only. Servers that require HTTPS continue to use verified HTTPS addresses from the account.

For LAN discovery, enable **Settings → Server → Network → Enable local network discovery (GDM)** in Plex and allow UDP port `32414` on the server network. Containers must expose discovery traffic to the LAN. If GDM is unavailable or receives no replies, discovery still checks the account’s advertised addresses. An explicit server address also works. See [Plex network settings](https://support.plex.tv/articles/200430283-network/).

The successful choice reconnects automatically on later launches. About → Connections → Use existing connection returns to it while another provider is active. Selecting Plex again opens a fresh picker. Back cancels selection and lets you return to the previous connection. Failed or canceled selection preserves the working server.

Account linking and refreshing the server list require internet access. Reopening a saved connection checks the media server directly. Network failures preserve sign-in. Missing or rejected server credentials return to account validation and selection. Automatic address recovery is not implemented. Use About → Connections → Plex to select an available address again.

Discovery stores account credentials in `state/discovery/plex/account/session.json`, the selected server in `state/discovery/plex/server.json`, and separate server credentials under `state/discovery/plex/servers/`. Tokens never appear in the picker, logs, or configuration. Explicit server profiles keep their existing sign-in locations and remain authoritative for those routes.

## Run locally

For discovery, use the [Ghostty discovery instructions](../tools/ghostty/README.md#plex-discovery). To choose a fixed server address instead, add a `server` section to your development `settings.json`. Preserve any other sections:

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

Plex credentials live in `plex/session.json` beneath the state directory. Jellyfin uses `session.json` in the state directory. Artwork and playback choices use separate provider identities, even if item IDs overlap. Switching to Jellyfin uses the same fields with `provider: "jellyfin"` and its URL. Removing `server` permits legacy `jellyfin.conf` fallback. Unknown providers stop startup instead of silently selecting another server.

## Playback and limits

The adapter requests progressive H.264 video with MP3 audio in Matroska, bounded by `server.transcode` (default 720×576 at 12 Mbps) and 30 fps for NTSC or 25 fps for PAL. Go opens the authenticated stream and passes bytes to the selected decoder. Resume and progress use Plex milliseconds translated to the shared timebase. Each prepared stream owns its timeline reporter and transcode cleanup.

Video preparation registers a Plex playback decision before opening the stream. The decision, stream, timeline reports, and cleanup use the same playback identity. A seek creates a new identity so the old stop report cannot terminate its replacement.

Subtitle tracks appear in the View menu. Plex burns image and embedded subtitle tracks into the video. Changing those tracks replaces the stream at the current position. Sidecar text subtitles download as UTF-8 SubRip and use the shared overlay without replacing playback. The adapter marks tracks that require burn-in through `MediaStream.RequiresBurnIn`. Jellyfin selects client rendering or server burn-in by subtitle codec.

Music uses artist → album → track navigation, ordered album queues, and whole-library shuffle. Original audio files pass through the authenticated loopback proxy with byte-range support. Shared controls handle pause, track changes, and music visuals. Album queues are bounded to 10,000 rows. Shuffle requests return up to 64 tracks per batch.

Testing on Ghostty and MiSTer covered sign-in, library navigation, Continue Watching, artwork, music and track navigation, photos, video seeking, subtitles, and Live TV. These checks do not establish compatibility with every source format or server setup. The shared transcode limits apply to both providers. Jellyfin's MPEG-2 codec selection does not apply to Plex.

Multi-file videos, Plex Home profile switching, automatic address recovery, relay connections, and Plex remote control are not implemented. The linked account determines library access. Account avatars and profile selection are not implemented.

## Photos

Photo libraries use their Plex names in the carousel. Open an album or folder, then select a photo to open the shared viewer. Previous/next skips folders and videos. Up toggles the controls, and Back returns to the containing list. Automatic slideshow playback is not implemented. Video clips within photo libraries use normal video playback.

Plex resizes original photos to the viewer dimensions, preserves aspect ratio, and applies EXIF orientation. The client bounds image downloads and decoding. Missing or invalid images show a retry message. Photos use the shared memory cache. Album artwork and carousel mosaics use the shared disk caches.

## Live TV

A Live TV carousel card appears when the linked account can access enabled channels on a Plex DVR. Select the card to open the channel list. Selecting a channel starts playback immediately. Back stops playback and returns to that list. Original/Zoom, buffering feedback, controls, and decoded closed captions use the same UX as Jellyfin. Seeking and timeshift are not supported.

When a tuned channel exposes selectable audio alternatives, View → Audio lists them with Plex’s labels. Changing audio briefly reloads at the live edge while keeping picture mode and the captions setting. Plex resolves each choice against the new session’s stream IDs before conversion. The choice applies to the current playback and is not saved in the client. Plex may remember the per-user selection.

Channels come from enabled DVR mappings, with duplicate tuner mappings removed and channel numbers sorted naturally. Protected channels are omitted when the tuner identifies them. Guide names, logos, and current program titles are optional. Without a guide, the list uses tuner names or channel numbers. A full schedule grid, recording controls, and Plex's free online Live TV service are not included.

Tuning uses the shared `media.LiveTV` interface. Tuner startup has a 30-second deadline. Ordinary metadata requests have a 15-second timeout. Diagnostics identify tune requests without logging private channel or consumer IDs.

Each attempt owns a unique Plex consumer and conversion session. Stopping, canceling, or failing playback releases both without canceling another client's consumer or a DVR recording. Conversion uses the configured dimensions and bitrate, capped at 30 fps for 240p, 30000/1001 fps for 480i, or 25 fps for PAL, matching the shared output cadence policy.

Live TV from an HDHomeRun has been tested through Plex on Ghostty and MiSTer, including captions and alternate audio. Automated tests cover missing guide data, paging, source geometry, isolated ownership, cancellation, and failed preparation.

## Other library types

| Type | Status |
| --- | --- |
| Movies and TV shows | Supported, including recorded TV stored in ordinary libraries. |
| Music | Supported, including album queues and shuffle. |
| Other Videos | Uses Plex's movie/clip types and the shared video path. |
| Photos | Supported, including album folders and previous/next navigation. |
| Collections and playlists | Carousel cards, collection hierarchy, and ordered playlist browsing and playback. See [browsing behavior](GO_BROWSING.md#collections-and-playlists). |

Collections use `/library/all?type=18` and `/library/collections/{id}/items`. Playlists use `/playlists` and `/playlists/{id}/items`. Both normal and smart playlists are included. Playlist contents keep the server order and duplicate entries. Local playback stays paged, so large playlists do not inherit the 10,000-item remote queue limit. Edit playlists and collections in Plex.

## Code boundaries

[Application assembly](../cmd/mistervision/server.go) selects a [`connection.Connector`](../internal/connection/connector.go). [`plex.Connector`](../internal/plex/connector.go) links the account and returns a [`media.Server`](../internal/media/server.go) implemented by `plex.Client`. Plex endpoints, response types, and transcode policy stay in [`internal/plex`](../internal/plex). Neither the browser nor the renderer imports the adapter. [`serverDiscovery`](../internal/plex/discovery.go) implements the shared `connection.Discoverer` interface. It retains server grants privately while the shared picker receives only names, identities, and addresses. [`gdmDiscovery`](../internal/plex/gdm.go) implements the same discovery interface for LAN addresses. Its anonymous UDP scan runs alongside the account request and stops on cancellation. [`discovered_connection.go`](../internal/plex/discovered_connection.go) coordinates account linking, selection, and saved reconnection.

[`serverstate`](../internal/serverstate/session.go) supplies private, atomic session storage to both adapters. The shared catalog and playback contracts cover video and music. `media.Artwork.Photo` supplies images to the shared photo viewer. The optional `media.LiveTV` contract covers tuner negotiation. [`live_channels.go`](../internal/plex/live_channels.go) discovers channels, [`live.go`](../internal/plex/live.go) owns tuner consumers, and [`transcode.go`](../internal/plex/transcode.go) shares conversion policy with recorded video. Provider-specific additions stay in the adapter.
