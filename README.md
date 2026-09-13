# MiSTerFin CRT

A Jellyfin client for movies, TV, live TV, music, and photos. It runs on MiSTer FPGA and in Ghostty on Linux, with the same interface on both.

The UI is built explicitly for CRT output. Maintainer trentnix uses a CRT for testing and everyday use.

![MiSTerFin CRT library carousel](docs/images/screenshots/home-carousel.png)

Current UI captures: [Continue Watching](docs/images/screenshots/continue-watching.png), [movie library](docs/images/screenshots/movies-list.png), and [movie details](docs/images/screenshots/movie-info.png).

## Run on MiSTer

Installation is manual. Build the client using the [build guide](docs/GO_BUILD.md) and its matching MPlayer using the [player build instructions](docs/GO_PLAYBACK.md#mister-use-and-remaining-work).

Copy these files to the SD card and make them executable:

| File | Destination |
| --- | --- |
| `build/misterfin-crt-arm` | `/media/fat/misterfin-crt/misterfin-crt` |
| `build/misterfin-crt-mplayer-arm` | `/media/fat/misterfin-crt/mplayer-arm` |
| [`tools/misterfin-crt.sh`](tools/misterfin-crt.sh) | `/media/fat/Scripts/MiSTerFin-CRT.sh` |

Create `/media/fat/misterfin-crt/jellyfin.conf` containing your server URL:

```text
http://your-jellyfin-server:8096
```

Launch **MiSTerFin-CRT** from the Scripts menu. Approve the displayed Quick Connect code in Jellyfin. The launcher filename must contain no spaces. Login, playback choices, and artwork caches persist on the SD card.

## Run in Ghostty

Requires Linux, Ghostty, Go 1.26, a C compiler, Python 3, libmpv, and FFmpeg.

```bash
git clone https://github.com/trentnix/misterfin-crt.git
cd misterfin-crt
```

Create `jellyfin.conf` in the repository directory with your server URL, as shown above. Then run:

```bash
python3 tools/ghostty/ghostty_harness.py --browse --config jellyfin.conf --ntsc --inline-video
```

The harness builds the client automatically. Approve the Quick Connect code in Jellyfin. Use `--pal` instead of `--ntsc` for the PAL layout.

To try browsing without a server or playable media:

```bash
python3 tools/ghostty/ghostty_harness.py --demo --ntsc
```

See the [Ghostty guide](tools/ghostty/README.md) for setup and options.

## Controls

Use arrows to navigate, Enter to select or pause, Escape to go back or stop, and Q to quit. During video or music playback, any arrow shows or hides controls. J/L seeks, and brackets change music tracks. On-screen hints show the available actions.

MiSTer controller bindings and button labels are [configurable](docs/GO_INPUT.md). Music backgrounds and meters are also [configurable](docs/GO_MUSIC.md).

## More information

- [Browsing, Continue Watching, and artwork caches](docs/GO_BROWSING.md)
- [Playback and controls](docs/GO_PLAYBACK.md)
- [Subtitles, audio tracks, and picture modes](docs/GO_TRACKS.md)
- [Builds and tests](docs/GO_BUILD.md)
- [Rendering architecture](docs/GO_RENDERING.md)

Possible future outputs include an SDL desktop window, direct Linux display through DRM/KMS, and a web browser. These are ideas, not scheduled features.

## Origins and license

Maintained independently by trentnix, based on [MiSTerFin](https://github.com/puddingstudio/MiSTerFin) by Pudding Studio and the maintainer's [C changes](https://github.com/trentnix/MiSTerFin). This is not an official Pudding Studio release. The original C application is preserved at `c-baseline`.

MiSTerFin-derived material remains under [CC BY-NC 4.0](LICENSE), copyright © 2026 Pudding Studio. Third-party components retain their [separate licenses](docs/THIRD_PARTY.md), including GPL terms for the patched MPlayer.
