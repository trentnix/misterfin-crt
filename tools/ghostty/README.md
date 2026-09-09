# Ghostty interactive harness

This helper presents MiSTerFin's existing desktop framebuffer inside Ghostty. MiSTerFin still reads the terminal directly, so the helper does not translate or intercept input.

From the repository root, run:

```bash
python3 tools/ghostty/ghostty_harness.py --ntsc
```

Use `--pal` for the 640x288 layout. PAL is the default. The helper builds the host binary before launch. Pass `--no-build` to use the existing binary.

## Go prototype

To browse the local demo, run:

```bash
python3 tools/ghostty/ghostty_harness.py --demo --ntsc
```

The demo starts a temporary mock Jellyfin server on loopback. It includes more than 500 movies, TV shows, music, Live TV channels, Home Videos, and a Mixed library. Configuration and session files stay in a temporary directory and are removed on exit. No real server or credentials are needed.

To browse a real Jellyfin server, create a `jellyfin.conf` containing its URL, then run:

```bash
python3 tools/ghostty/ghostty_harness.py --browse --ntsc --config jellyfin.conf
```

The browser displays a Quick Connect code. Approve that code in Jellyfin to sign in. The existing three-line server URL, API key, and username format also works. Go saves its session separately under the user configuration directory in `misterfin-go/session.json`. It does not read or overwrite the C client's token or device files. `--state-dir PATH` selects another Go session directory.

Go browser controls:

- Up and Down select an item.
- B, Enter, or X opens a library, folder, or item summary. On a video details screen, B starts or resumes playback in a separate FFplay window.
- During playback, A in Ghostty stops the player and returns to details. A keypress in the video window closes that window.
- Desktop playback requires `ffplay`. See [the playback guide](../../docs/GO_PLAYBACK.md). The mock-server demo provides browsing data, not playable media.
- A, Escape, Backspace, or Z goes back or cancels loading.
- Left and Right move between home cards. In lists, Left and Right or Page Up and Page Down jump one screen.
- Tab (SELECT) toggles the home carousel and library list.
- Back at home opens an exit confirmation. B confirms and A cancels.
- R retries a failed request or sign-in.
- Q or Ctrl+C exits.

Use `--pal` for PAL. The Go browser draws lists, artwork, and item summaries. Playback, full item details, and the C client's remaining screens are not ported yet. The preview works without MiSTer hardware.

To view the original Go test frame, run:

```bash
python3 tools/ghostty/ghostty_harness.py --go --ntsc
```

Use `--go --pal` for PAL. The test frame displays color bars, a grayscale ramp, and a white border. Press Ctrl+C to exit. It needs no Jellyfin configuration and uses the same C framebuffer adapter as the browser, with allocated headless memory in place of `/dev/fb0`.

The default command without `--go`, `--browse`, or `--demo` continues to run the C client. The navigation keys and browsing features below apply to that client.

## C client controls

Keys match the desktop harness:

- Arrow keys navigate.
- `B`, Enter, or `X` confirms, matching the on-screen B label.
- `A`, Escape, Backspace, or `Z` goes back, matching the on-screen A label.
- Tab is Select.
- Home or `P` is Start.
- Page Up or `[` is the left shoulder button.
- Page Down or `]` is the right shoulder button.
- `Q` exits.

The helper writes MiSTerFin's stdout and stderr to `/tmp/misterfin-ghostty.log` so terminal output cannot corrupt the image. Pass `--log PATH` to choose another location.

Artwork is cached under `/tmp/misterfin-cache` by default. Set `MISTERFIN_CACHE_ROOT` before launching the helper to use another location.

Ghostty must report `TERM=xterm-ghostty`. The `--force` option permits another terminal that implements the Kitty graphics protocol.

The viewer double-buffers terminal images to avoid flicker. It uploads a complete frame under an alternate image ID, places the new frame over the current frame, and only then deletes the old frame. MiSTerFin's 640x240 and 640x288 framebuffers use non-square CRT pixels, so the viewer fits them into a physical 4:3 rectangle using the terminal's cell geometry. The viewer caps presentation at 20 FPS by default and skips duplicate frames. Change the cap with `--fps NUMBER`. This cap only affects the terminal preview. It does not change MiSTerFin's own frame loop.

Video playback remains unavailable in the desktop harness because `mplayer` opens `/dev/fb0` directly. Browsing, artwork, menus, setup, and metadata use the headless framebuffer and are visible.

Run the helper tests with:

```bash
python3 -m unittest tools/ghostty/test_ghostty_harness.py
```
