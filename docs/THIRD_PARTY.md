# Third-party code and components

MiSTerFin-derived application code remains under [CC BY-NC 4.0](../LICENSE). The license file includes separate copyright notices for Pudding Studio's original material and trentnix's additions and modifications. Third-party components and modifications to them keep the terms listed below.

## Bundled code

- **Go runtime and standard library** — the Go Authors, BSD-style license. Release bundles include the license from the compiler used to build the client as `licenses/go-LICENSE`.

- **[coder/websocket](https://github.com/coder/websocket) v1.8.15** — Coder, ISC license. The Go executable uses this library for Jellyfin remote control over WebSocket, including HTTPS. Its [copyright and permission notice](licenses/coder-websocket.txt) applies to this component.
- **[font8x8](https://github.com/dhepper/font8x8)** — Daniel Hepper, public domain, based on the IBM VGA font via Marcel Sondaar. The retained header is `docker/font8x8.h`. The Go bitmap tables in `internal/ui/font.go` were translated from the C baseline's identical header. The ASCII table retains its public-domain terms. The MiSTerFin Latin-1 extensions retain CC BY-NC 4.0 and Pudding Studio's copyright notice.
- **[MPlayer](https://mplayerhq.hu) 1.5** — the MPlayer team, GPL-2.0-or-later. MiSTer playback uses an external MPlayer process built by `docker/Dockerfile.misterfin-crt` and `docker/build-mplayer.sh`. Its corresponding source consists of the upstream 1.5 release, `docker/vo_fbdev.c`, `docker/vo_fbdev_go.patch`, `docker/vo_fbdev_interlaced.patch`, `docker/mplayer_go.patch`, `docker/mplayer_overlay_refresh.patch`, `docker/mplayer_picture.patch`, `docker/swscale_arm_return.patch`, `docker/mplayer_captions.patch`, `docker/misterfin_captions.h`, and `docker/vf_misterfin.c`. The framebuffer driver retains the original MPlayer copyright notices. The driver, patches to MPlayer, and resulting executable remain under the GPL. `tools/testdata/mplayer-video-timing.c` and `tools/testdata/mplayer-overlay-command.c` contain upstream excerpts used to test playback timing and paused redraws. Both retain their GPL notices.

The native adapter in `internal/platform/adapter_linux.c` derives framebuffer geometry and presentation from MiSTerFin. It retains CC BY-NC 4.0 and Pudding Studio's copyright notice.

Release bundles include the application license, the Go license, the coder/websocket notice, and MPlayer/FFmpeg license texts. The accompanying source archive includes the committed project and the exact upstream MPlayer archive used for the binary. Its `docker/` directory contains the modifications and build recipes. See [release bundles](GO_BUILD.md#release-bundles).

## External components

- **[Main_MiSTer](https://github.com/MiSTer-devel/Main_MiSTer)** provides the MiSTer environment and enables framebuffer output when launching from the Scripts menu.
- **[Izzie Walton's interlaced Menu core](https://github.com/iwalton3/Menu_MiSTer/releases/tag/v0.0.1)** supplies optional interlaced output and retains its own license. See [display setup](GO_DISPLAY.md). Zaparoo integration is deferred and is not required for this mode.
- Desktop playback uses externally installed FFmpeg tools and, for inline video, libmpv. Those components retain their own licenses and are not included in the Go executable.

## Original C application

The [MiSTerFin integration repository](https://github.com/trentnix/MiSTerFin) preserves the C application. The [documentation index](README.md) identifies the merged reference branch and commit. The C repository's `src/stb_image.h` contains **[stb_image](https://github.com/nothings/stb) v2.30**, by Sean Barrett and contributors, dual-licensed MIT / public domain and used by the C client as public domain. The Go client uses Go image decoders instead.

Historical references to `src/` identify files in that C source checkout. The original framebuffer implementation is `src/fb.c`.

## Reference and inspiration

- **[jellyfin-apiclient-python](https://github.com/jellyfin/jellyfin-apiclient-python)** — reference for the shape of the MediaBrowser authorization header. No code copied.
- **Ryan Geiss** — the C baseline's Nebula music visualizer was inspired by his classic feedback visualizers. No code copied.
