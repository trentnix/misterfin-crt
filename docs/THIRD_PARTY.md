# Third-party code and components

MiSTerFin-derived application code remains under [CC BY-NC 4.0](../LICENSE), with Pudding Studio's copyright notices retained. Third-party components keep their own terms.

## Bundled code

- **[font8x8](https://github.com/dhepper/font8x8)** — Daniel Hepper, public domain, based on the IBM VGA font via Marcel Sondaar. The retained header is `docker/font8x8.h`. The Go bitmap tables in `internal/ui/font.go` were translated from the C baseline's identical header. The ASCII table retains its public-domain terms. The MiSTerFin Latin-1 extensions retain CC BY-NC 4.0 and Pudding Studio's copyright notice. `tools/gen_font.py` and `tools/gen_subfont.py` generate the MPlayer font atlases in `assets/` from the retained header.
- **[MPlayer](https://mplayerhq.hu) 1.5** — the MPlayer team, GPL-2.0-or-later. MiSTer playback uses an external MPlayer process built by `docker/Dockerfile.misterfin-go` and `docker/build-mplayer.sh`. Its corresponding source consists of the upstream 1.5 release, `docker/vo_fbdev.c`, `docker/vo_fbdev_go.patch`, `docker/mplayer_go.patch`, `docker/mplayer_picture.patch`, and `docker/vf_misterfin.c`. The framebuffer driver retains the original MPlayer copyright notices. The driver, patches to MPlayer, and resulting executable remain under the GPL. `tools/testdata/mplayer-video-timing.c` contains the upstream timing excerpt used to test the playback fix and retains its GPL notice.

The native adapter in `internal/platform/adapter_linux.c` derives framebuffer geometry and presentation from MiSTerFin. It retains CC BY-NC 4.0 and Pudding Studio's copyright notice.

## External components

- **[Main_MiSTer](https://github.com/MiSTer-devel/Main_MiSTer)** provides the MiSTer environment and enables framebuffer output when launching from the Scripts menu.
- **[Zaparoo Project](https://zaparoo.org)** and **[Izzie Walton's interlaced Menu core](https://github.com/iwalton3/Menu_MiSTer/releases/tag/v0.0.1)** provide alternative display environments described in the inherited hardware compatibility guide. Those projects retain their own licenses. A reference in that guide does not establish support in the Go client.
- Desktop playback uses externally installed FFmpeg tools and, for inline video, libmpv. Those components retain their own licenses and are not included in the Go executable.

## Preserved C baseline

The `c-baseline` tag retains the original application, documentation, and third-party notices. The removed `src/stb_image.h` contains **[stb_image](https://github.com/nothings/stb) v2.30**, by Sean Barrett and contributors, dual-licensed MIT / public domain and used by the C client as public domain. The Go client uses Go image decoders instead.

Historical references to `src/` identify files at that tag. For example, `git show c-baseline:src/fb.c` displays the original framebuffer implementation.

## Reference and inspiration

- **[jellyfin-apiclient-python](https://github.com/jellyfin/jellyfin-apiclient-python)** — reference for the shape of the MediaBrowser authorization header. No code copied.
- **Ryan Geiss** — the C baseline's Nebula music visualizer was inspired by his classic feedback visualizers. No code copied.
