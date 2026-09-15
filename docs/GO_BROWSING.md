# Go browsing prototype

The Go client supports Jellyfin browsing, artwork, a combined Continue Watching home card, and media playback in Ghostty and on MiSTer. The maintainer tests on a CRT. The C source and launcher remain the behavioral reference.

## Try the browser

```sh
python3 tools/ghostty/ghostty_harness.py --demo --ntsc
```

The demo builds the Go host binary and starts the existing mock Jellyfin server on an ephemeral loopback port. It uses temporary configuration and session files. No server setup is required. Use `--pal` to select 640×288 instead of 640×240.

To connect to a real server, create a configuration file with the server URL on the first line. A reverse-proxy path such as `https://example.test/jellyfin` is supported. Run:

```sh
python3 tools/ghostty/ghostty_harness.py --browse --ntsc --config jellyfin.conf
```

Approve the displayed Quick Connect code in another Jellyfin client. Alternatively, put the API key and username on the second and third lines. Blank lines and comments are ignored. `PAL`, `NTSC`, and `INSECURE_TLS` are recognized independently of line position. A `WIDTHxHEIGHT[@BITRATE]` line configures [video conversion limits](GO_PLAYBACK.md#transcode-configuration). `DEBUGLOG` enables [optional diagnostics](GO_DIAGNOSTICS.md). The harness's PAL/NTSC flag controls headless geometry.

Go stores its device identity and token in `misterfin-crt/session.json` under `os.UserConfigDir()`, normally `$XDG_CONFIG_HOME` or `$HOME/.config` on Linux. `--state-dir` overrides that directory. Session writes use a private temporary file and atomic rename. Saved sessions are bound to the configured server URL. The C client's `token.conf` and `device.conf` are not imported or changed.

| Key | Action |
| --- | --- |
| Up / Down | Select an item |
| B / Enter / X | Open library, folder, or item summary |
| A / Escape / Backspace / Z | Go back or cancel loading |
| Left / Right or Page Up / Page Down | Move between home cards, or jump one screen in lists |
| Tab (SELECT) | Toggle the home carousel and library list |
| R | Retry request or sign-in |
| Q / Ctrl+C | Exit and restore terminal settings |

The original test frame remains available with `--go --ntsc`. The harness's default command continues to run the C client.

## Behavior and boundaries

`internal/jellyfin` owns HTTP, JSON, configuration, sessions, and artwork decoding. `internal/browser` owns navigation and request lifetimes. `internal/ui` draws BGRX text and artwork in Go using translated bitmap data from the inherited font. `internal/terminal` reads Linux terminal keys without cgo and restores terminal settings on shutdown. The existing platform adapter still owns framebuffer presentation.

Movies and Music Videos use recursive lists filtered to their item types. Music retains artist, album, and track traversal. Home Videos and Mixed libraries retain folders and omit expensive folder user data.

Mixed libraries request the selected parent's direct children without an item-type filter. The inherited mixed-type whitelist caused Jellyfin to include unrelated music artists and an extra backing-folder row in Nostalgia. Removing that whitelist restores the library's actual contents while keeping movie, series, and nested-folder navigation.

Other collections retain the C query's count fields. Series and seasons use the Jellyfin Shows endpoints. Live TV uses the C client's channel query, preserves server order, and shows channel numbers and current programs. If Jellyfin omits a Live TV view, the browser probes one channel and adds a Live TV entry when channels are available. Server-provided library names remain unchanged, including custom names for Live TV.

The home screen uses a horizontal library carousel with Jellyfin library names, a dimmed, moving cover mosaic, and the selected library's item count. Names use MiSTerFin's 160-pixel limit at text scale 2, with an ellipsis for longer names. Counts use the C client's separate recursive `Limit=0` query and collection-specific item filter. Cover samples do not supply counts. Missing or failed counts stay unknown. Live TV has no carousel count, matching C. Tab toggles the classic library list without losing selection. Back at home opens the C client's exit confirmation. B confirms exit and A cancels.

Lists keep the selected row near the center while the rows scroll behind it. At the beginning and end of the library, the highlight moves toward the first or last row. Lists use the C font spacing, colors, CRT safe margins, 30-pixel rows, top-aligned covers, and watched/resume metadata. The clock updates while idle. Long headers scroll, and selections animate. Left and Right jump six rows in NTSC or seven rows in PAL. Up and Down scroll one row at a time.

Server pages contain 64 items. Each paginated view retains a contiguous window of up to three pages. Within 24 rows of a loaded edge, a worker prefetches the neighboring page. Page arrivals preserve absolute selection and scroll positions, and reversing direction reuses retained rows. If a page falls behind, current rows remain visible with a small loading message. Navigation can reverse while the request is pending. Background failures do not interrupt navigation or trigger a retry loop. The server's total count controls further paging when available. Without a count, a full page permits another request. Failed pages preserve the current rows and retry the failed offset. Back navigation preserves the parent's selection. Canceling a request or leaving a view invalidates its generation so a late response cannot replace the current screen. Metadata and library counts update independently as soon as their requests finish. Covers, backdrops, and logos arrive separately, with at most three image requests in flight. List artwork and carousel cover samples retain a 120 ms selection debounce. Details metadata and counts have no debounce. Each selection has its own cancellation token, and completed images remain cached when navigation cancels other requests.

Saved tokens survive network failures, invalid JSON, and server errors. Only an explicit HTTP 401 or 403 triggers replacement Quick Connect authentication. HTTP requests time out after 15 seconds, limit responses to 8 MiB, and reject redirects to another origin. Artwork dimensions are checked before image decoding. Tokens, Quick Connect secrets, and server response bodies are not written to logs. TLS certificates are verified unless the configuration explicitly contains `INSECURE_TLS`.

Item details fetch the overview, year, rating, runtime, and watched/resume state. The screen uses a fading backdrop and a transparent logo when available. Pressing B starts or resumes movies, episodes, videos, and music videos. Selecting a Live TV channel tunes it immediately. Stopping or ending channel playback returns to the channels list with the selection preserved. Photos open full screen. Selecting an audio track starts album playback with artwork and a progress bar. Photos support Left/Right navigation across pages. Up toggles photo controls. During music and video playback, any direction toggles controls, triggers or J/L seek, and shoulders or brackets change music tracks. Music and desktop video pause or resume with B/Enter without displaying instructions. Desktop playback opens a separate FFplay window by default. Add `--inline-video` to play inside Ghostty through libmpv. A in Ghostty stops playback and returns to movie details, the channels list, or the track list, depending on the playing item. See [the playback guide](GO_PLAYBACK.md). The UI has no search. Continue Watching combines resumable videos and next episodes in one home card. The [About page](GO_ABOUT.md) shows the installed version and public release availability. Update installation remains a placeholder. The setup starfield, exact carousel transition timing, and full C feature parity remain pending. Library video supports LT/RT or J/L seeking in 30-second steps. Music uses 10-second steps. Recorded video supports subtitle and audio-track selection through SELECT/Tab. See [track controls](GO_TRACKS.md). Music supports whole-library shuffle, stereo level meters, and configurable backgrounds. See [music configuration](GO_MUSIC.md). Text supports ASCII and Latin-1. Other characters use a fallback glyph. Artwork uses a session cache keyed by item ID, image kind, and image tag. The cache holds at most 16 MiB of decoded pixels and 128 images, evicting the least recently used images. Cached images appear immediately across lists and details, including after playback. Metadata refreshes after playback so watched and resume state remain current. Counts and cover samples are cached for one minute for up to 32 libraries. R refreshes the selected item without clearing unrelated artwork. Carousel collages and ordinary covers, backdrops, and logos also persist to disk as described below. Photos remain in the session cache. The C application remains the reference.

## Continue Watching

The first home card is labeled `Continue` to fit the C carousel's name width. It opens a `Continue Watching` list containing unfinished videos and the next unwatched episodes of series in progress. Continue occupies the first carousel position while its initial feed loads and shows `Loading...` instead of an item count. Feed arrival fills the card without changing selection, so startup does not briefly select another library. Browsing other libraries and opening Continue remain available while the request runs. Empty results remove the card and preserve the selected library, or select the first library if Continue was selected.

Each series appears once. A resumable episode takes precedence over its next episode. If several episodes are resumable, the most recently played episode wins. Entries show the series and episode title, with `Resume · 12:34 · S1 E3` or `Next · S1 E4` beneath them. Movies show their resume position. Opening an entry uses the existing details screen, resume/play behavior, and SELECT/Tab restart action.

The client fetches `/UserItems/Resume` with `MediaTypes=Video` and `/Shows/NextUp` with `enableResumable=false` concurrently. It reads each source's pages before merging, so duplicates do not distort the combined count or paging. Each source is bounded to roughly 10,000 entries, with an explicit error if the bound is reached before completion. The merged list is a retained snapshot and uses the normal centered scrolling behavior.

Recent playback puts entries near the front. A separate bounded query reads the 256 most recently played episodes to supply series activity dates without requesting each series separately. Resumable videos also supply their own last-played dates. If a series has no known recent date, it follows dated entries and retains Jellyfin's Next Up ordering. A failed optional history query keeps the feed usable with that ordering fallback. [Jellyfin's Next Up implementation](https://github.com/jellyfin/jellyfin/blob/master/Emby.Server.Implementations/TV/TVSeriesManager.cs) orders episodes using the series' last-watched activity internally.

Home requests run independently of library and selection requests. A slow feed does not block library browsing. The feed refreshes when opened, after library-video playback ends, and when returning to the home screen or combined list. Refreshes preserve selection by item ID, then series ID if the episode changes. R retries a failed feed. A partial result stays usable with an error message, and a completely failed refresh retains the previous snapshot.

The Continue card uses up to twelve covers from the current feed. Its cover metadata comes from the feed rather than a library-count or mosaic query. Individual images share the artwork memory and disk caches. This changing home collage is not persisted as a combined file, and its synthetic ID is never sent to Jellyfin as a library or item ID.

## Persistent artwork cache

Covers, backdrops, and logos persist under `/media/fat/misterfin-crt/covercache` on MiSTer, alongside the C application's separate `covercache`. The existing `MISTERFIN_CACHE_ROOT` override applies to both artwork and collages. Go appends `misterfin-crt/covercache` and a server/user partition. Ghostty's default root is `/tmp/misterfin-cache`. Set `MISTERFIN_CACHE_ROOT` to persistent storage if its cache must survive a reboot.

The first visit downloads each image. Later visits and application launches reuse decoded RGBA pixels without image requests or JPEG/PNG decoding. Logo transparency is preserved. Keys include the image owner, kind, Jellyfin image tag, and cache format version. Episodes can reuse their parent's backdrop. Changed tags fetch new pixels. Metadata still comes from Jellyfin, so cached images do not make the browser an offline client. Full-screen photos retain their existing memory cache and are not stored in the ordinary-artwork disk cache.

Each server/user partition retains at most 512 images and 128 MiB, separately from the collage budget. Oldest-written images are pruned first. Reads do not touch timestamps or rewrite files. The worker scans the directory once when it first saves an image, then maintains its inventory in memory. Checksums and size limits reject damaged files, and atomic replacement prevents partially written images from becoming visible. An unavailable or unwritable cache falls back to normal image loading.

R invalidates the selected item's ordinary artwork and its shared parent backdrop. The next worker removes the saved entries and fetches replacements. Canceled or superseded requests cannot overwrite those replacements. Disk reads, writes, and pruning run in artwork workers. The browser's immediate snapshots continue to use only the bounded memory cache.

## Persistent collage cache

On MiSTer, Go saves carousel collages under `/media/fat/misterfin-crt/gridcache`, alongside the C application's `/media/fat/misterfin/gridcache`. Go uses a separate format and directory. Saved collages survive application restarts and reboots. The first visit to a library still downloads its images to populate the cache.

The cache stores decoded RGBA cover images and their Jellyfin IDs and image tags. After a restart, a worker restores the saved collage before refreshing the sample metadata. Unchanged tags reuse saved pixels without image downloads or JPEG/PNG decoding. Changed tags fetch only the changed images, including when the library count remains the same. A failed metadata refresh keeps the saved collage visible. Counts continue to refresh independently.

Each server/user partition retains at most 32 collages and 64 MiB. Oldest written entries are evicted first. Versioned files have size limits and checksums, and writes replace files atomically. Incomplete or canceled loads do not overwrite a usable collage. Corrupt files and unavailable storage fall back to normal loading. Unchanged collages do not rewrite the SD card.

The shared `MISTERFIN_CACHE_ROOT` override places Go collages under `$MISTERFIN_CACHE_ROOT/misterfin-crt/gridcache`. The Ghostty harness defaults that root to `/tmp/misterfin-cache`, so its cache survives application restarts but not a reboot. Direct headless Go runs use the user cache directory when the override is absent. Set the override to persistent storage if desktop caches must survive reboot. R invalidates the selected collage on its next worker load.

To choose another location on MiSTer, add an export to the Go launcher before the command that starts the application. For example, if a USB drive is mounted at `/media/usb0`, use:

```sh
export MISTERFIN_CACHE_ROOT=/media/usb0
```

Go then writes collages under `/media/usb0/misterfin-crt/gridcache`. The variable selects the parent directory. Go appends `misterfin-crt/gridcache` for collages or `misterfin-crt/covercache` for ordinary artwork, followed by an account partition. The application must be able to write to that location. Changing the root populates a new cache and leaves the previous cache in place.

For a Ghostty cache that survives reboot, use:

```sh
MISTERFIN_CACHE_ROOT="$HOME/.cache" python3 tools/ghostty/ghostty_harness.py --browse --ntsc --config jellyfin.conf
```

Go then writes collages under `$HOME/.cache/misterfin-crt/gridcache`. The harness preserves an explicitly set `MISTERFIN_CACHE_ROOT`.

## Validation

```sh
make test
make test-browse
go test -race ./...
go vet ./...
make arm
```

The HTTP tests and demo require loopback sockets. A sandbox that prohibits sockets must allow those tests to run outside that restriction.

Go tests cover complete collection query parameters, path/query encoding, response validation, cancellation, redirect rejection, saved-token preservation, Quick Connect replacement, server-bound storage, navigation generations, failed-page retries, terminal escape sequences, Latin-1 drawing, and artwork aspect ratio. The browser integration tests run the built Go binary in a pseudoterminal against the inherited mock server and exercise paging across the movie library, Music hierarchy, and Back during delayed loading.

Go tests passed with cgo enabled and disabled. Race tests, `go vet`, 18 Ghostty helper tests, both browser integration tests, and the ARM cross-build passed. Host frames were inspected for libraries, movies, paging, and item summaries. The subsequent UX pass adds checks for carousel controls, exit confirmation, backward page crossings, CRT geometry, metadata requests, and transparent artwork. The user confirmed that the demo browser works in Ghostty. Real-server authentication and browsing remain unverified until a server is supplied. Hardware display testing remains deferred because of the previously recorded kernel framebuffer failure. See `docs/GO_BUILD.md` for that first-milestone record.

The library-query corrections have regression checks for custom library names, exact count and cover queries for every collection type, unknown counts, Live TV discovery, channel order and guide fields, and the C season, episode, and details queries. Real-server results still require user verification.

Responsiveness checks hold image responses open to verify that metadata, counts, and completed covers reach the UI independently. They also verify the three-request image limit, cache reuse between screens, fresh metadata after playback, cancellation, expiry, and eviction. The Ghostty demo server handles concurrent connections so a slow artwork request cannot block its other API requests.

In a read-only timing sample against the configured Jellyfin server on September 9, 2026, details metadata arrived in 146 ms and all three detail images arrived by 230 ms. The carousel count arrived in 115 ms, its first cover in 287 ms, and all covers by 693 ms. Cached revisits required no downloads. These measurements cover loader delivery, not complete input-to-display latency. Full Go tests with and without cgo, race checks, vet, the four browser integration tests, the Ghostty demo capture, and the ARM build passed after the responsiveness changes.
