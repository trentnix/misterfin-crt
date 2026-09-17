# Ghostty interactive harness

This helper presents MiSTerVision's desktop framebuffer inside Ghostty. MiSTerVision reads the terminal directly, so the helper does not translate or intercept input.

Use Linux, Go 1.26.8 or later, a C compiler, and Python 3. Playback dependencies are listed below. See the [build guide](../../docs/GO_BUILD.md) for toolchain setup. From the repository root, run:

```bash
python3 tools/ghostty/ghostty_harness.py --demo --ntsc
```

Use `--pal` for the 640x288 layout. PAL is the default. The helper builds the host binary before launch. Pass `--no-build` to use the existing binary.

## Browsing

To browse the local demo, run:

```bash
python3 tools/ghostty/ghostty_harness.py --demo --ntsc
```

The demo starts a temporary mock Jellyfin server on loopback. It includes more than 500 movies, TV shows, music, Live TV channels, Home Videos, and a Mixed library. Configuration and session files stay in a temporary directory and are removed on exit. No real server or credentials are needed.

To connect to Jellyfin or Plex, create a `settings.json` with a `server` section as shown in the [project README](../../README.md#run-on-mister), then run:

```bash
python3 tools/ghostty/ghostty_harness.py --browse --ntsc --inline-video --settings /path/to/settings.json
```

Approve Jellyfin Quick Connect in a signed-in Jellyfin client, or enter the Plex code at `plex.tv/link`. Sessions default to the user configuration directory under `mistervision`. Plex uses its `plex` subdirectory. `--state-dir PATH` selects another state directory. For an existing MiSTerFin CRT development setup, pass its old state directory explicitly or follow the [rename instructions](../../docs/GO_BUILD.md#moving-from-misterfin-crt).

Legacy Jellyfin configurations still work with `--config jellyfin.conf`. Application options default to `settings.json` beside that file. `--settings PATH` overrides `MISTERVISION_SETTINGS`. See [configuration](../../docs/GO_CONFIGURATION.md).

Go browser controls:

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

Use `--pal` for PAL. The Go browser draws lists, artwork, and item summaries. The Go client also supports video, music, and photos. See the playback guide for controls and remaining limits. The preview works without MiSTer hardware.

To view the original Go test frame, run:

```bash
python3 tools/ghostty/ghostty_harness.py --go --ntsc
```

Use `--go --pal` for PAL. The test frame displays color bars, a grayscale ramp, and a white border. Press Ctrl+C to exit. It needs no Jellyfin configuration and uses the same C framebuffer adapter as the browser, with allocated headless memory in place of `/dev/fb0`.

Without `--browse` or `--demo`, the helper shows the Go test frame. The `--go` flag remains accepted for existing commands.

The helper writes MiSTerVision's stdout and stderr to `/tmp/mistervision-ghostty.log` so terminal output cannot corrupt the image. Pass `--log PATH` to choose another location.

The artwork cache defaults to `/tmp/mistervision-cache`. Carousel collages use its `mistervision/gridcache` directory. Covers, backdrops, and logos use `mistervision/covercache`. Both survive application restarts, but `/tmp` does not survive reboot. Set `MISTERVISION_CACHE_ROOT` before launching the helper to use persistent storage. See [the Go collage cache](../../docs/GO_BROWSING.md#persistent-collage-cache) for freshness checks and limits.

Ghostty must report `TERM=xterm-ghostty`. The `--force` option permits another terminal that implements the Kitty graphics protocol.

The viewer double-buffers terminal images to avoid flicker. It uploads a complete frame under an alternate image ID, then places the new frame and deletes the old frame within one synchronized terminal update. The upload stays outside that update so the current image remains visible while data transfers. MiSTerVision's 640x240 and 640x288 framebuffers use non-square CRT pixels, so the viewer fits them into a physical 4:3 rectangle using the terminal's cell geometry. The viewer skips duplicate frames.

The presentation cap defaults to 20 FPS, or 60 FPS with `--inline-video`. Change the cap with `--fps NUMBER`. The presenter wakes when the Go frame file is complete, then uploads changed frames up to the configured cap. Upload time counts toward each interval. If an upload overruns a deadline, the presenter skips expired slots rather than building a backlog. This cap affects the terminal preview and does not change the decoder's playback clock.

Press F1 while browsing to open About. Esc or F1 returns to the preceding screen. Tab or R checks for updates, and Enter opens release notes when a release is available. Up/Down scrolls the notes. Desktop installations show a manual-installation message. Automatic installation is limited to the standard MiSTer installation.

Add `--inline-video` to a real-server browsing command to play video inside Ghostty. Inline playback requires libmpv and FFmpeg. Without that flag, video opens in a separate FFplay window. See [desktop playback setup](../../docs/GO_PLAYBACK.md).

Run the helper tests with:

```bash
python3 -m unittest tools/ghostty/test_ghostty_harness.py
```

Run browser integration tests with `make test-browse`. Each test in [test_go_browse.py](test_go_browse.py) explicitly starts a [Scenario](fixtures/browser.py) with its settings, mock-server behavior, and simulated player. Ordinary tests write `settings.json` directly. One explicit scenario checks legacy files. The standalone player programs in [fixtures](fixtures/) publish controlled frames and playback feedback without opening a media decoder or audio device. These tests need the Go host build and loopback networking, but do not need Ghostty or a Jellyfin server. Before sending input that depends on loaded data, wait for the corresponding `browser.page` or `browser.home` diagnostic event. A completed HTTP response alone does not mean the browser has applied the result.
