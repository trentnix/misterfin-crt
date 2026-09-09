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
| Left / Right or Page Up / Page Down | Previous or next page |
| R | Retry request or sign-in |
| Q / Ctrl+C | Exit and restore terminal settings |

The original test frame remains available with `--go --ntsc`. The harness's default command continues to run the C client.

## Behavior and boundaries

`internal/jellyfin` owns HTTP, JSON, configuration, sessions, and artwork decoding. `internal/browser` owns navigation and request lifetimes. `internal/ui` draws BGRX text and artwork in Go using translated bitmap data from the inherited font. `internal/terminal` reads Linux terminal keys without cgo and restores terminal settings on shutdown. The existing platform adapter still owns framebuffer presentation.

Movies and Music Videos use recursive lists filtered to their item types. Music retains artist, album, and track traversal. Home Videos and Mixed libraries retain folders and omit expensive folder user data. Other collections retain the C query's count fields. Series and seasons use the Jellyfin Shows endpoints. Live TV views returned by the server list channels, but the prototype does not synthesize a missing Live TV root view.

Pages contain 64 items. The server's total count controls further paging when available. Without a count, a full page permits another request. Failed pages preserve the current rows and retry the failed offset. Back navigation preserves the parent's selection. Canceling a request or leaving a view invalidates its generation so a late response cannot replace the current screen. Artwork loads separately with a short selection debounce and its own cancellation token.

Saved tokens survive network failures, invalid JSON, and server errors. Only an explicit HTTP 401 or 403 triggers replacement Quick Connect authentication. HTTP requests time out after 15 seconds, limit responses to 8 MiB, and reject redirects to another origin. Artwork dimensions are checked before image decoding. Tokens, Quick Connect secrets, and server response bodies are not written to logs. TLS certificates are verified unless the configuration explicitly contains `INSECURE_TLS`.

The UI provides lists, Primary artwork, watched/resume markers, and item summaries. It has no playback, photo viewer, complete metadata screen, search, Continue Watching, Next Up, or controller input. Text supports ASCII and Latin-1. Other characters use a fallback glyph. Artwork is fetched on selection without a persistent cache. These limits keep the initial browsing path reviewable while preserving the C application as the reference.

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

Go tests passed with cgo enabled and disabled. Race tests, `go vet`, 18 Ghostty helper tests, both browser integration tests, and the ARM cross-build passed. Host frames were inspected for libraries, movies, paging, and item summaries. The user confirmed that the demo browser works in Ghostty. Real-server authentication and browsing remain unverified until a server is supplied. Hardware display testing remains deferred because of the previously recorded kernel framebuffer failure. See `docs/GO_BUILD.md` for that first-milestone record.
