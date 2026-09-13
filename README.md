# MiSTerFin-Go

An unofficial Go adaptation of [MiSTerFin](https://github.com/puddingstudio/MiSTerFin) by Pudding Studio, maintained independently by trentnix. The starting point includes the improvements merged into the `local-all-features` branch of [trentnix/MiSTerFin](https://github.com/trentnix/MiSTerFin).

The UI is built explicitly for CRT output. The project maintainer, trentnix, uses a CRT for both testing and everyday use.

MiSTerFin-Go supports Jellyfin authentication, library browsing, Continue Watching, video and Live TV playback, photos, and music. The same Go UI renders to the MiSTer framebuffer or a Ghostty terminal. MiSTer uses a separate patched MPlayer process for playback. Ghostty can play video through libmpv with `--inline-video`, or in a separate FFplay window.

## Try it in Ghostty

```bash
python3 tools/ghostty/ghostty_harness.py --demo --ntsc
```

The demo needs no Jellyfin server. To connect to your server, put its URL in `jellyfin.conf` and run:

```bash
python3 tools/ghostty/ghostty_harness.py --browse --config jellyfin.conf --ntsc --inline-video
```

Approve the displayed Quick Connect code in Jellyfin. Use `--pal` for PAL output. Without `--demo` or `--browse`, the harness displays a static test frame. See the [Ghostty guide](tools/ghostty/README.md) for dependencies and controls.

## Build and test

The host build requires Go 1.26 and a C compiler. The tests also use Python 3 and `patch`. ARM cross-compilation uses Zig. See [GO_BUILD.md](docs/GO_BUILD.md) for toolchain details.

```sh
make host
make test
make test-browse
make arm
```

The binaries are `build/misterfin-go` and `build/misterfin-go-arm`. Existing `make -f Makefile.port` commands remain supported. CI builds and tests Go. Automated release packaging, installation, and updater integration remain pending.

## Guides

- [Browsing, authentication, and artwork caching](docs/GO_BROWSING.md)
- [Controller and keyboard configuration](docs/GO_INPUT.md)
- [Video, photos, music, and MiSTer player builds](docs/GO_PLAYBACK.md)
- [Music backgrounds and meters](docs/GO_MUSIC.md)
- [Rendering architecture](docs/GO_RENDERING.md)
- [Port plan and provenance](docs/GO_PORT_PLAN.md)
- [CRT hardware compatibility](docs/DISPLAY_COMPATIBILITY.md) — inherited hardware reference. Application-specific instructions describe the C client.

## C reference

The C application and its tests are preserved at the `c-baseline` tag, commit `19d99fa5f479692e45ea7b5dddc42e42fb1782a9`. They are no longer included in the working tree. To inspect the original application in a separate checkout:

```sh
git worktree add --detach ../MiSTerFin-C-reference c-baseline
```

The remaining C code supports the Go framebuffer adapter and the external MPlayer build. The player timing regression fixture also remains. Font generators use `docker/font8x8.h`.

## License

MiSTerFin-derived material remains under [CC BY-NC 4.0](LICENSE). Copyright © 2026 Pudding Studio. Third-party components retain their [separate licenses](docs/THIRD_PARTY.md). This adaptation is not an official Pudding Studio release.
