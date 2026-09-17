# Browsing and sign-in

Use the [MiSTer launcher](GO_BUILD.md#install-on-mister) or [development harness](../tools/ghostty/README.md). The home carousel shows server library names and a combined Continue Watching card. Tab/SELECT switches between the carousel and root library list.

## Setup and sign-in

Connection settings normally come from `settings.json`. Invalid JSON connection settings stop startup with a field-level error. Restart after correcting them.

When no `server` section exists, the client reads legacy `jellyfin.conf` if present. A missing legacy file starts Jellyfin discovery. An invalid legacy file opens setup help instead of selecting a different server. Connection, disabled Quick Connect, unknown username, and sign-in storage failures have separate recovery instructions. Open/Enter retries after you correct the file. R is a retry alias. Back returns to discovery when sign-in followed a discovery selection. Otherwise, Back opens the connection chooser when connections are available, or exits setup. Start/F1 opens About during discovery, connection attempts, approval, and errors. Choosing another connection cancels the unfinished attempt. Canceling from the connection chooser restores the last connected browser, or exits if none exists.

Jellyfin Quick Connect displays a public approval code. Enter it in an already signed-in Jellyfin client. The waiting indicator animates until approval or the five-minute timeout. New code cancels the previous attempt. The screen does not expose credentials or Quick Connect secrets.

Plex uses an account-link code at `plex.tv/link`. The linked account determines access to the configured server. [Plex support](GO_PLEX.md) describes sign-in and provider-specific limits.

The standard MiSTer state directory is `/media/fat/mistervision/state`. Desktop defaults to `~/.config/mistervision`, or `mistervision` under `XDG_CONFIG_HOME` when set. The executable's `-state-dir` or harness's `--state-dir` overrides that directory. Jellyfin stores identity and sign-in in `session.json`. Plex uses `plex/session.json`.

Saved sessions are bound to the server URL. C `token.conf` and `device.conf` files are not imported.

For a complete, valid saved session, network and server failures retain its tokens. An explicit HTTP 401 or 403 triggers replacement authentication. Damaged local sign-in data is backed up before a fresh sign-in, with a notice explaining the recovery. Storage failures show setup help instead. See [saved sign-in recovery](GO_CONFIGURATION.md#saved-sign-in-recovery). TLS verification is enabled unless [configured otherwise](GO_CONFIGURATION.md#server-connection).

## Jellyfin discovery

When no server is configured, the client first checks its remembered Jellyfin selection. If none exists, it scans directly connected IPv4 networks for three seconds using UDP port 7359. The picker shows server names and addresses, including when only one server answers. Up/Down selects a row, Open connects, Select/Tab or R scans again, and Back opens connection choices. Quick Connect follows selection. During connection, approval, or a sign-in failure after selection, Back cancels the attempt and scans again so you can choose another server. Back from the connection chooser restores the previous connected browser, or exits if none exists. Back is also available when a later launch reuses the remembered server but still needs sign-in.

The selection is stored in `jellyfin-server.json` under the state directory. Later launches connect to that address and reuse valid sign-in. Discovery does not create or edit configuration files. An explicit JSON server or an existing legacy configuration always takes precedence. Invalid explicit configuration never triggers discovery. An unreadable or damaged saved selection shows recovery instructions and is preserved.

If no servers appear, make sure Jellyfin discovery is enabled and UDP port 7359 can reach the server. Containers must expose that UDP port. Broadcast discovery normally stays on the local subnet. Retry after fixing the network, or set `server.url` in `settings.json` using the [connection example](GO_CONFIGURATION.md#server-connection). If connecting to a remembered address fails, MiSTerVision scans once for the same server ID and checks its public identity without sending credentials. A matching new address opens **Server address changed**. Select it to reconnect with your saved sign-in, or press Back to cancel. The new address is remembered after sign-in succeeds. Failed recovery preserves your sign-in and offers Retry. Explicitly configured addresses never change automatically, and HTTPS connections cannot recover to HTTP. Use About → Connections to choose a different server.

## Navigation

| Action | Controller | Keyboard |
| --- | --- | --- |
| Select a row | Up/Down | Up/Down |
| Change home card or jump a list screen | Left/Right | Left/Right or Page Up/Page Down |
| Open | B | Enter, B, or X |
| Back | A | Escape, Backspace, A, or Z |
| Switch home view | Select | Tab |
| About | Start | F1 |
| Retry | Configured retry binding | R |
| Quit | Configured quit binding | Q or Ctrl+C |

On-screen badges follow the active [input profile](GO_INPUT.md). Back at home opens exit confirmation.

Held directions accelerate. Lists keep selection near the center except at the first and last rows. Neighboring pages load ahead, and arrivals preserve the selected position. A late page leaves existing rows visible with a loading message. Back restores the parent selection.

For Jellyfin, movies and music videos use filtered recursive lists. Music retains artist → album → track navigation. Mixed and home-video libraries retain folders. Series and seasons use Jellyfin's Shows endpoints. Live TV preserves server channel order and opens a channel directly into playback. Stopping returns to that channel in the list.

Details show available artwork, overview, year, rating, runtime, and resume/watched state. Open starts or resumes video. SELECT/Tab restarts an unwatched resumable video from the beginning. See [playback controls](GO_PLAYBACK.md#playback-controls).

Menu text supports Latin-1 and selected additional characters. Subtitles use a broader Unicode font set. See [text coverage](GO_RENDERING.md#text-coverage). Search is not implemented.

## Continue Watching

The first card, labeled Continue, combines resumable videos and the next unwatched episode of series in progress. For Jellyfin, each series appears once. Its most recently played resumable episode takes precedence over Next Up. Recent playback orders dated entries first. Undated series retain Jellyfin's Next Up order.

For Plex, the card uses the first 100 entries of the server's combined feed from `/hubs/continueWatching/items`, keeping movies and episodes.

The card reserves its position while loading, so startup does not switch away from a briefly selected library. Empty results remove it while preserving library selection. A slow feed does not block other libraries.

Opening the card, returning home, and finishing recorded playback refresh the feed. Refreshes preserve the selected item or series. Partial failures keep usable results. R retries.

The Jellyfin adapter merges `/UserItems/Resume` and `/Shows/NextUp`, with a bounded recent-episode query for ordering. It reads pages before deduplication. Each source has an approximately 10,000-entry safety limit. Continue covers use the ordinary artwork cache. The changing combined collage is not stored as a library collage.

## Collections and playlists

Collections and Playlists appear in the carousel only when nonempty and enabled by [`ui.show_collections` and `ui.show_playlists`](GO_CONFIGURATION.md#carousel-categories). Both settings default to `true`. Jellyfin's existing card names are preserved. Collections retain their hierarchy, so a collection can contain movies, shows, albums, or other folders. Open a playlist to browse its entries in server order. Repeated entries remain separate rows.

Select a music track or video to start playback. Playback advances through the remaining audio/video entries, fetching pages as needed. The first selected video uses its normal resume position. Subsequent entries start at the beginning. Stopping or reaching the end returns to the list with the current entry selected. Music retains previous/next controls. Videos retain their existing pause and seek controls.

Photo playlists use the manual photo viewer. Automatic slideshows and playlist editing are not implemented.

Locally started playlists publish the current item to Jellyfin remote controls and support previous/next commands. Sending a playlist from another Jellyfin client uses the existing bounded remote queue.

Books, comics, and audiobook categories are hidden from the carousel. Display names do not determine library type. Plex has no dedicated audiobook category, so audiobooks stored as an ordinary music library remain indistinguishable from music.

## Photos

Photos open full screen with preserved proportions. Left/Right moves through photos, including across pages. Up toggles controls, which expire after three seconds. Back restores the folder with the current photo selected. R retries a failed image. Slideshows and photo zoom are not implemented.

## About and updates

Start or F1 opens and closes About while browsing. Back also closes it. About is unavailable during media playback and loading a media item. It remains available during setup. Press Down for Connections, then choose an existing connection, Jellyfin discovery, or Plex setup. See [multiple connections](GO_CONFIGURATION.md#multiple-connections). It shows the embedded logo, installed version, and credits for Pudding Studio's original material and Trent Nix's changes, with the [license](../LICENSE) and [component notice](THIRD_PARTY.md).

The client checks this repository's latest public stable release once per launch. Select/Tab or R checks again after the preceding request finishes. Stable `vMAJOR.MINOR.PATCH` versions are compared numerically. Development builds can offer a public release without claiming it is newer than the checkout. Builds use the version described in the [build guide](GO_BUILD.md#go-client).

No GitHub credentials are sent. A missing or inaccessible release displays “No public release available.” Network, rate-limit, and invalid-response failures display “Could not check for updates.” Neither means the installation is current. If an update is offered, Open shows its release notes. Up/Down scrolls the notes. Open again starts installation on a standard MiSTer installation. Desktop and custom installations show a manual-installation message.

Release authors can put concise on-screen text under `## Release summary` in the GitHub release description. Use plain paragraphs and optional third-level headings. The app displays that section through the next first- or second-level heading. Put manual-installation instructions under a separate `## Installation` heading. Missing or empty summaries fall back to the full notes. Selected text is limited to 8,192 characters.

The updater downloads and verifies the release, backs up the installed files, then replaces the client and matching player. Back cancels during download or validation. During replacement, wait for completion. Success shows “Update installed. Restarting...” for two seconds before the app restarts. Custom launchers without restart support show a manual-relaunch message instead.

Failures restore the previous files. Startup recovers an interrupted replacement before opening the display. Settings, sign-in, playback choices, caches, and the optional 480i core stay intact. See [installation and recovery](GO_BUILD.md#application-updates).

## Persistent artwork cache

Covers, backdrops, and logos persist under `mistervision/covercache` within the cache root, partitioned by provider, server, and user. The first visit downloads images. Later visits and launches reuse decoded pixels. Image tags invalidate changed artwork. Metadata still requires the media server, so caching does not provide offline browsing. Full-screen photos use memory caching only.

| Cache | Per-account limit |
| --- | --- |
| Decoded image memory | 128 images / 16 MiB |
| Ordinary artwork disk cache | 512 images / 128 MiB |
| Library collage disk cache | 32 collages / 64 MiB |

Disk files have version, size, and checksum checks and are replaced atomically. Oldest-written entries are pruned first. Reads do not rewrite timestamps. Corrupt entries become misses.

Unavailable storage leaves browsing usable without disk caching. R invalidates the selected artwork for replacement. Disk work stays on workers, outside rendering.

## Persistent collage cache

Library mosaics persist under `mistervision/gridcache` in the same cache root. A worker restores saved pixels before refreshing sample IDs and image tags. Only changed images download again. Counts refresh independently. Incomplete loads do not replace a usable collage, and unchanged collages do not rewrite the SD card.

Default cache roots are `/media/fat` on MiSTer, the user's cache directory for direct desktop runs, and `/tmp/mistervision-cache` in the Ghostty harness. Set `MISTERVISION_CACHE_ROOT` to change the parent directory. Go appends `mistervision/covercache` or `mistervision/gridcache` and the account partition.

For a persistent desktop cache:

```sh
MISTERVISION_CACHE_ROOT="$HOME/.cache" python3 tools/ghostty/ghostty_harness.py --browse --ntsc --inline-video --settings settings.json
```

On MiSTer, export the variable in the launcher before starting the client. A new root populates a new cache and leaves old files intact. Custom [browsing backgrounds and titles](GO_CONFIGURATION.md#browsing-background) are separate settings.
