# MiSTerFin CRT

MiSTerFin CRT is a Jellyfin client for MiSTer FPGA, designed for CRT televisions. It supports movies, TV, live TV, music, and photos.

I built MiSTerFin CRT to make Jellyfin enjoyable to use on my CRT. I test and use it on a MiSTer connected to a consumer 4:3 CRT television, not a PVM or an HD set. The interface is designed for that screen.

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

## Controls

Use the D-pad to navigate and follow the on-screen button hints to select or go back. During video or music playback, any direction shows or hides controls. Triggers seek, and shoulder buttons change music tracks.

Controller bindings and button labels are [configurable](docs/GO_INPUT.md). The [playback guide](docs/GO_PLAYBACK.md#playback-controls) lists controller and keyboard controls. Music backgrounds and meters are also [configurable](docs/GO_MUSIC.md).

## Local development and testing

I use the Ghostty harness on Linux to develop and test the interface without MiSTer hardware. It also helps verify that the architecture supports different display pipelines while reusing the same UI and application logic. From the repository directory, run the browsing demo:

```bash
python3 tools/ghostty/ghostty_harness.py --demo --ntsc
```

The harness builds the client automatically. See the [development harness guide](tools/ghostty/README.md) for dependencies, connecting to Jellyfin, and testing playback.

## More information

- [Browsing, Continue Watching, and artwork caches](docs/GO_BROWSING.md)
- [Playback and controls](docs/GO_PLAYBACK.md)
- [Subtitles, audio tracks, and picture modes](docs/GO_TRACKS.md)
- [Builds and tests](docs/GO_BUILD.md)
- [Rendering architecture](docs/GO_RENDERING.md)

Possible future outputs include an SDL desktop window, direct Linux display through DRM/KMS, and a web browser. These are ideas, not scheduled features.

## Origins and license

MiSTerFin CRT is heavily based on [MiSTerFin](https://github.com/puddingstudio/MiSTerFin) by Pudding Studio. It began as a Go port that included trentnix's [C changes](https://github.com/trentnix/MiSTerFin). Trentnix maintains the project independently.

Original MiSTerFin material is copyright © 2026 Pudding Studio. MiSTerFin CRT additions and modifications by trentnix are copyright © 2026 trentnix. This material is distributed under [CC BY-NC 4.0](LICENSE). Third-party components retain their [separate licenses](docs/THIRD_PARTY.md), including GPL terms for the patched MPlayer.
