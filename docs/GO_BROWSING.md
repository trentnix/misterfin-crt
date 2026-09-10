# Go browsing prototype

The second milestone provides a desktop browsing path in Ghostty: configuration, API-key or Quick Connect authentication, libraries, paginated item lists, artwork, and item summaries. MiSTer validation remains deferred at the user's request. The C source and its launcher remain unchanged. The README and port plan now describe the prototype's progress while preserving the project provenance.

## Try the browser

```sh
python3 tools/ghostty/ghostty_harness.py --demo --ntsc
```

The demo builds the Go host binary and starts the existing mock Jellyfin server on an ephemeral loopback port. It uses temporary configuration and session files. No server setup is required. Use `--pal` to select 640×288 instead of 640×240.

To connect to a real server, create a configuration file with the server URL on the first line. A reverse-proxy path such as `https://example.test/jellyfin` is supported. Run:

```sh
python3 tools/ghostty/ghostty_harness.py --browse --ntsc --config jellyfin.conf
```

Approve the displayed Quick Connect code in another Jellyfin client. Alternatively, put the API key and username on the second and third lines. Blank lines and comments are ignored. `PAL`, `NTSC`, and `INSECURE_TLS` are recognized independently of line position. The inherited transcode-profile and `DEBUGLOG` lines are accepted but have no effect in this browsing prototype. The harness's PAL/NTSC flag controls headless geometry.

Go stores its device identity and token in `misterfin-go/session.json` under `os.UserConfigDir()`, normally `$XDG_CONFIG_HOME` or `$HOME/.config` on Linux. `--state-dir` overrides that directory. Session writes use a private temporary file and atomic rename. Saved sessions are bound to the configured server URL. The C client's `token.conf` and `device.conf` are not imported or changed.

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

Movies and Music Videos use recursive lists filtered to their item types. Music retains artist, album, and track traversal. Home Videos and Mixed libraries retain folders and omit expensive folder user data. Other collections retain the C query's count fields. Series and seasons use the Jellyfin Shows endpoints. Live TV uses the C client's channel query, preserves server order, and shows channel numbers and current programs. If Jellyfin omits a Live TV view, the browser probes one channel and adds a Live TV entry when channels are available. Server-provided library names remain unchanged, including custom names for Live TV.

The home screen uses a horizontal library carousel with Jellyfin library names, a dimmed, moving cover mosaic, and the selected library's item count. Names use MiSTerFin's 160-pixel limit at text scale 2, with an ellipsis for longer names. Counts use the C client's separate recursive `Limit=0` query and collection-specific item filter. Cover samples do not supply counts. Missing or failed counts stay unknown. Live TV has no carousel count, matching C. Tab toggles the classic library list without losing selection. Back at home opens the C client's exit confirmation. B confirms exit and A cancels.

Lists use the C font spacing, colors, CRT safe margins, 30-pixel rows, top-aligned covers, and watched/resume metadata. The clock updates while idle. Long headers scroll, and selections animate. Left and Right jump six rows in NTSC or seven rows in PAL. Up and Down scroll one row at a time.

Server pages contain 64 items. The server's total count controls further paging when available. Without a count, a full page permits another request. Failed pages preserve the current rows and retry the failed offset. Back navigation preserves the parent's selection. Canceling a request or leaving a view invalidates its generation so a late response cannot replace the current screen. Metadata and library counts update independently as soon as their requests finish. Covers, backdrops, and logos arrive separately, with at most three image requests in flight. List artwork and carousel cover samples retain a 120 ms selection debounce. Details metadata and counts have no debounce. Each selection has its own cancellation token, and completed images remain cached when navigation cancels other requests.

Saved tokens survive network failures, invalid JSON, and server errors. Only an explicit HTTP 401 or 403 triggers replacement Quick Connect authentication. HTTP requests time out after 15 seconds, limit responses to 8 MiB, and reject redirects to another origin. Artwork dimensions are checked before image decoding. Tokens, Quick Connect secrets, and server response bodies are not written to logs. TLS certificates are verified unless the configuration explicitly contains `INSECURE_TLS`.

Item details fetch the overview, year, rating, runtime, and watched/resume state. The screen uses a fading backdrop and a transparent logo when available. Pressing B starts or resumes movies, episodes, videos, and music videos. Pressing B on a Live TV channel’s details tunes the channel. Photos open full screen. Selecting an audio track starts album playback with artwork and a progress bar. Photos support Left/Right navigation across pages. Up reveals photo, music, and desktop video controls for three seconds. Music and desktop video pause or resume with B/Enter without displaying instructions. Desktop playback opens a separate FFplay window by default. Add `--inline-video` to play inside Ghostty through libmpv. A in Ghostty stops playback and returns to details. See [the playback guide](GO_PLAYBACK.md). The UI has no search, Continue Watching, Next Up, or controller input. About, the setup starfield, exact carousel transition timing, and full C feature parity remain pending. Video playback still needs seeking, track selection, and hardware overlays. Music still needs shuffle and visualizers. Text supports ASCII and Latin-1. Other characters use a fallback glyph. Artwork uses a session cache keyed by item ID, image kind, and image tag. The cache holds at most 16 MiB of decoded pixels and 128 images, evicting the least recently used images. Cached images appear immediately across lists and details, including after playback. Metadata refreshes after playback so watched and resume state remain current. Counts and cover samples are cached for one minute for up to 32 libraries. R refreshes the selected item without clearing unrelated artwork. The caches do not persist to disk. The C application remains the reference.

## Validation

```sh
make -f Makefile.port test
make -f Makefile.port test-browse
go test -race ./...
go vet ./...
make -f Makefile.port arm
```

The HTTP tests and demo require loopback sockets. A sandbox that prohibits sockets must allow those tests to run outside that restriction.

Go tests cover complete collection query parameters, path/query encoding, response validation, cancellation, redirect rejection, saved-token preservation, Quick Connect replacement, server-bound storage, navigation generations, failed-page retries, terminal escape sequences, Latin-1 drawing, and artwork aspect ratio. The browser integration tests run the built Go binary in a pseudoterminal against the inherited mock server and exercise paging across the movie library, Music hierarchy, and Back during delayed loading.

Go tests passed with cgo enabled and disabled. Race tests, `go vet`, 18 Ghostty helper tests, both browser integration tests, and the ARM cross-build passed. Host frames were inspected for libraries, movies, paging, and item summaries. The subsequent UX pass adds checks for carousel controls, exit confirmation, backward page crossings, CRT geometry, metadata requests, and transparent artwork. The user confirmed that the demo browser works in Ghostty. Real-server authentication and browsing remain unverified until a server is supplied. Hardware display testing remains deferred because of the previously recorded kernel framebuffer failure. See `docs/GO_BUILD.md` for that first-milestone record.

The library-query corrections have regression checks for custom library names, exact count and cover queries for every collection type, unknown counts, Live TV discovery, channel order and guide fields, and the C season, episode, and details queries. Real-server results still require user verification.

Responsiveness checks hold image responses open to verify that metadata, counts, and completed covers reach the UI independently. They also verify the three-request image limit, cache reuse between screens, fresh metadata after playback, cancellation, expiry, and eviction. The Ghostty demo server handles concurrent connections so a slow artwork request cannot block its other API requests.

In a read-only timing sample against the configured Jellyfin server on September 9, 2026, details metadata arrived in 146 ms and all three detail images arrived by 230 ms. The carousel count arrived in 115 ms, its first cover in 287 ms, and all covers by 693 ms. Cached revisits required no downloads. These measurements cover loader delivery, not complete input-to-display latency. Full Go tests with and without cgo, race checks, vet, the four browser integration tests, the Ghostty demo capture, and the ARM build passed after the responsiveness changes.
