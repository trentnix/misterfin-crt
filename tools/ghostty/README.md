# Ghostty interactive harness

This helper presents MiSTerVision's desktop framebuffer inside Ghostty. MiSTerVision reads the terminal directly, so the helper does not translate or intercept input.

## Requirements and demo

Run these commands in a Ghostty terminal on Linux, from the repository root. Install Go 1.26.8 or later, Make, a C compiler, and Python 3. Music and inline video also need the libmpv shared library. Install FFmpeg for the separate-window FFplay fallback and decoder tests. No Python packages are required. See the [build guide](../../docs/GO_BUILD.md) for build commands.

Check the inline playback dependency:

```bash
python3 tools/ghostty/video_player.py --check
```

To try browsing without a media server:

```bash
python3 tools/ghostty/ghostty_harness.py --demo --ntsc
```

Use `--pal` for the 640x288 layout, which is the default. The terminal preview does not validate PAL signal timing on a CRT. The helper builds the host binary before launch. Pass `--no-build` to use the existing binary.

## Browsing

The demo starts a temporary mock Jellyfin server on loopback. It includes more than 500 movies, TV shows, music, Live TV channels, Home Videos, and a Mixed library. Configuration and session files stay in a temporary directory and are removed on exit. No real server or credentials are needed.

### Jellyfin discovery

Use a separate profile so old settings in the repository cannot bypass discovery:

```bash
profile="$HOME/.config/mistervision/discovery"
mkdir -p "$profile"
python3 tools/ghostty/ghostty_harness.py --browse --ntsc --inline-video \
  --config "$profile/jellyfin.conf" \
  --state-dir "$profile/state"
```

Do not create `jellyfin.conf` or add a `server` section to this profile's `settings.json`. The `--config` argument selects a legacy configuration path that is deliberately absent. Other application settings can go in `settings.json` beside it, which the client loads automatically.

Select a server, then approve Quick Connect in an already signed-in Jellyfin client. Before sign-in completes, Escape/Back returns to discovery so you can choose another server. Back remains available if you exit without approving and launch again. Escape on the server picker opens connection choices. Back from those choices restores a previous connection or exits if none exists. Close and reopen with the same command to verify that the server and sign-in are remembered. If no server appears, see [discovery and troubleshooting](../../docs/GO_BROWSING.md#jellyfin-discovery).

### Plex discovery

Use a separate profile with no server address:

```bash
profile="$HOME/.config/mistervision/plex-discovery"
mkdir -p "$profile"
python3 tools/ghostty/ghostty_harness.py --browse --ntsc --inline-video \
  --config "$profile/jellyfin.conf" \
  --state-dir "$profile/state"
```

Leave `jellyfin.conf` absent. During the initial Jellyfin discovery screen, press F1, then Down for Connections, and choose Plex. Approve the code at [plex.tv/link](https://plex.tv/link). For Plex Home, choose a viewer and enter their PIN if requested, then choose a server. Reopen with the same command to use the remembered server and viewer. Protected viewers must enter their PIN again.

About → Connections → Plex opens a fresh selection. **Sign in with another account** on the Plex server picker starts a new account link. Back cancels that link and returns to the saved account’s picker. See [discovery behavior and limits](../../docs/GO_PLEX.md#server-discovery).

### Explicit Jellyfin or Plex connection

Use separate profiles when testing both providers. For Jellyfin, create `~/.config/mistervision/jellyfin/settings.json` with:

```json
{
  "server": {
    "provider": "jellyfin",
    "url": "http://your-jellyfin-server:8096"
  }
}
```

For Plex, create `~/.config/mistervision/plex/settings.json` with:

```json
{
  "server": {
    "provider": "plex",
    "url": "http://your-plex-server:32400"
  }
}
```

Replace the example address with your server's address. Create the parent directory if needed. If the file already exists, preserve its other settings. Launch Jellyfin with:

```bash
profile="$HOME/.config/mistervision/jellyfin"
python3 tools/ghostty/ghostty_harness.py --browse --ntsc --inline-video \
  --config "$profile/jellyfin.conf" \
  --settings "$profile/settings.json" \
  --state-dir "$profile/state"
```

For Plex, change the first line to `profile="$HOME/.config/mistervision/plex"` and run the same command. Approve Jellyfin Quick Connect in a signed-in Jellyfin client, or enter the Plex code at [plex.tv/link](https://plex.tv/link). Plex account linking requires internet access.

To switch accounts inside one running client, use [named connections in one settings file](../../docs/GO_CONFIGURATION.md#multiple-connections), then open F1 → Down for Connections. The separate folders above remain useful for isolated testing. These directory names are examples, not built-in modes or Plex Home viewers.

The arguments select configuration and state paths. Each profile keeps its sign-in and playback preferences under `state`. Explicit Plex sign-in uses `state/plex`, while discovery uses `state/discovery/plex`. Without `--state-dir`, sessions use the user configuration directory under `mistervision`. For an existing MiSTerFin CRT setup, follow the [rename instructions](../../docs/GO_BUILD.md#moving-from-misterfin-crt).

Legacy Jellyfin configurations still work with `--config jellyfin.conf`. Application options default to `settings.json` beside that file. `--settings PATH` overrides `MISTERVISION_SETTINGS`. See [configuration](../../docs/GO_CONFIGURATION.md) for additional options.

## Controls and troubleshooting

Keyboard controls:

- Up and Down select an item.
- B, Enter, or X opens a library, folder, or item summary. On a video details screen, B starts or resumes playback.
- During playback, any arrow toggles the menu. J/L seeks backward/forward by 30 seconds for video or 10 seconds for music. Brackets or Page Up/Page Down change music tracks. B/Enter pauses or resumes without showing controls. A/Escape stops playback. Keep focus in Ghostty when using a separate video window.
- Inline video and controllable music require libmpv. Separate-window video requires `ffplay`. See [the playback guide](../../docs/GO_PLAYBACK.md). The mock-server demo provides browsing data, not playable media.
- A, Escape, Backspace, or Z goes back or cancels loading.
- Left and Right move between home cards. In lists, Left and Right or Page Up and Page Down jump one screen.
- Tab (SELECT) toggles the home carousel and library list.
- Back at home opens an exit confirmation. B confirms and A cancels.
- R retries a failed request or sign-in.
- Q or Ctrl+C exits.

To view a framebuffer test without a server, run:

```bash
python3 tools/ghostty/ghostty_harness.py --go --ntsc
```

Use `--go --pal` for the 640x288 test frame. The test frame displays color bars, a grayscale ramp, and a white border. Press Ctrl+C to exit. It needs no server configuration and uses the same C framebuffer adapter as the browser, with allocated headless memory in place of `/dev/fb0`.

Without `--browse` or `--demo`, the helper shows the Go test frame. The `--go` flag remains accepted for existing commands.

The helper writes MiSTerVision's stdout and stderr to `/tmp/mistervision-ghostty.log` so terminal output cannot corrupt the image. Pass `--log PATH` to choose another location.

The artwork cache defaults to `/tmp/mistervision-cache`. Carousel collages use its `mistervision/gridcache` directory. Covers, backdrops, and logos use `mistervision/covercache`. Both survive application restarts, but `/tmp` does not survive reboot. Set `MISTERVISION_CACHE_ROOT` before launching the helper to use persistent storage. See [collage caching](../../docs/GO_BROWSING.md#persistent-collage-cache) for freshness checks and limits.

Ghostty must report `TERM=xterm-ghostty`. The `--force` option permits another terminal that implements the Kitty graphics protocol.

The viewer double-buffers terminal images to avoid flicker. It uploads a complete frame under an alternate image ID, then places the new frame and deletes the old frame within one synchronized terminal update. The upload stays outside that update so the current image remains visible while data transfers. MiSTerVision's 640x240 and 640x288 framebuffers use non-square CRT pixels, so the viewer fits them into a physical 4:3 rectangle using the terminal's cell geometry. The viewer skips duplicate frames.

The presentation cap defaults to 20 FPS, or 60 FPS with `--inline-video`. Change the cap with `--fps NUMBER`. The presenter wakes when the Go frame file is complete, then uploads changed frames up to the configured cap. Upload time counts toward each interval. If an upload overruns a deadline, the presenter skips expired slots rather than building a backlog. This cap affects the terminal preview and does not change the decoder's playback clock.

Press F1 while browsing to open About. Esc or F1 returns to the preceding screen. Down opens Connections. Tab or R checks for updates, and Enter opens release notes when a release is available. Up/Down scrolls the notes. Desktop installations show a manual-installation message.

For Plex Home, Up opens **Switch profile**. Directions and Enter operate the profile picker and numeric keypad. The fourth digit submits the PIN, and Escape returns to the profiles. An incorrect-PIN message clears when you select the first digit of another attempt.

Add `--inline-video` to a real-server browsing command to play video inside Ghostty. Inline playback requires libmpv. Without that flag, video opens in a separate FFplay window. See [desktop playback setup](../../docs/GO_PLAYBACK.md).

Run the helper tests with:

```bash
python3 -m unittest tools/ghostty/test_ghostty_harness.py
```

Run browser integration tests with `make test-browse`. Each test in [test_go_browse.py](test_go_browse.py) explicitly starts a [Scenario](fixtures/browser.py) with its settings, mock-server behavior, and simulated player. Ordinary tests write `settings.json` directly. One explicit scenario checks legacy files. The standalone player programs in [fixtures](fixtures/) publish controlled frames and playback feedback without opening a media decoder or audio device.

These tests need the Go host build and loopback networking, but do not need Ghostty or a media server. Before sending input that depends on loaded data, wait for the corresponding `browser.page` or `browser.home` diagnostic event. A completed HTTP response alone does not mean the browser has applied the result.
