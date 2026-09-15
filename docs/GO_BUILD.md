# Build and install

Use Go 1.26.8 or later, a C compiler, and Python 3 on Linux. The Go module dependency is pinned in [go.mod](../go.mod). MiSTer builds also need an ARM cross-compiler. Docker builds the separate MPlayer executable on the development machine, not on MiSTer.

## Go client

```sh
make host
make arm
# If Zig is outside PATH:
ZIG=/absolute/path/to/zig make arm
```

The outputs are `build/misterfin-crt` and `build/misterfin-crt-arm`. The ARM target enables cgo and uses `GOOS=linux GOARCH=arm GOARM=7`. The [compiler wrapper](../tools/zig-cc-go.sh) targets `arm-linux-gnueabihf.2.31` and Cortex-A9. Zig 0.14.1 has been tested. `GO_ARM_CC` can select another compatible compiler. Set `GOCACHE` and `ZIG_GLOBAL_CACHE_DIR` if their default directories are unwritable.

Development builds show `dev`, the Git revision, and a modified marker when available. Jellyfin HTTP and WebSocket requests report the same version label, without the revision suffix. Set `VERSION` for a stable release label:

```sh
make arm VERSION=v1.0.0
```

## MPlayer

Build the matching patched player from [Dockerfile.misterfin-crt](../docker/Dockerfile.misterfin-crt):

```sh
make native-player
```

The outputs are `build/misterfin-crt-mplayer-arm` and its source/compiler record, `build/misterfin-crt-mplayer-build.txt`. The base image is pinned by digest, and the build verifies the MPlayer source archive with SHA-256. The Bullseye toolchain targets MiSTer's glibc 2.31. The patches provide shared overlays, picture changes, captions, interlaced presentation, and playback timing fixes. The original C client's player cannot substitute for this build. Update both binaries together when their protocol changes. See [third-party notices](THIRD_PARTY.md) for corresponding source and licenses.

For a release, build both executables from the same checkout and record the pair:

```sh
make arm VERSION=v1.0.0
make native-player
make release-manifest
```

`build/release-manifest.txt` records Go build metadata, MPlayer source and compiler details, and both executable checksums. Keep it with the release artifacts. The Go vulnerability scan does not audit the separately compiled native dependencies.

## Install on MiSTer

Copy these files to the SD card and make them executable:

| File | Destination |
| --- | --- |
| `build/misterfin-crt-arm` | `/media/fat/misterfin-crt/misterfin-crt` |
| `build/misterfin-crt-mplayer-arm` | `/media/fat/misterfin-crt/mplayer-arm` |
| [`tools/misterfin-crt.sh`](../tools/misterfin-crt.sh) | `/media/fat/Scripts/MiSTerFin-CRT.sh` |

Create `jellyfin.conf` beside the binaries with your server URL. Add optional [settings](GO_CONFIGURATION.md) in the same directory. Launch **MiSTerFin-CRT** from Scripts so Main_MiSTer enables framebuffer output. Launcher filenames must contain no spaces. An SSH launch alone does not perform the Scripts display setup.

The launcher enables both CPU cores, hides the console cursor, and reloads the normal menu after a successful exit. Failures leave their messages visible. Login and playback choices persist under `/media/fat/misterfin-crt/state`. Caches use separate [artwork directories](GO_BROWSING.md#persistent-artwork-cache). For 480i, follow the [display guide](GO_DISPLAY.md).

Exit before replacing binaries. Copy replacements to temporary filenames in the installation directory, set executable permissions, then rename them over the installed files. Installation and updates are manual.

If migrating from `misterfin-go`, copy its state, caches, server configuration, settings, and referenced assets into the corresponding `misterfin-crt` directories. On desktop, use the user configuration and cache directories. Preserve the old installation until verified, and reconcile existing destinations before copying. [Settings migration](GO_CONFIGURATION.md#migration) combines legacy JSON files.

## Local development

```sh
python3 tools/ghostty/ghostty_harness.py --demo --ntsc
```

The demo builds the client and serves mock browsing data. It does not provide playable media. See the [harness guide](../tools/ghostty/README.md) for dependencies and real-server playback.

For a framebuffer test without Jellyfin:

```sh
make headless
python3 tools/ghostty/ghostty_harness.py --go --ntsc
```

`make headless` writes `build/go-frame.raw` and `build/go-frame.png` at 640×288. The Ghostty command shows the color bars at 640×240 until interrupted. Direct test-frame runs accept `-headless WIDTHxHEIGHT`, `-output PATH`, and either `-hold 10s` or `-wait`. Hardware test frames need a hold or wait option to remain visible.

## Tests and CI

Install a C compiler, Python 3, FFmpeg, libmpv, and the libavcodec/libavutil development headers. Then run:

```sh
make host
make lint
make vulnerability-check
make test
go test -race ./...
make test-browse
```

`make lint` checks formatting, runs `go vet`, and uses pinned Staticcheck. [Linter settings](../staticcheck.conf) preserve proper-name capitalization in errors. `make vulnerability-check` uses pinned govulncheck to check reachable Go advisories. It does not scan native MPlayer dependencies.

`make test` covers Go with and without cgo plus Python/native adapter tests. `make test-browse` runs the built client against isolated HTTP/WebSocket fixtures. Generated media and local servers avoid a Jellyfin account or MiSTer dependency. Decoder tests can skip when their external dependencies are absent.

| CI job | Checks and triggers | Timeout |
| --- | --- | --- |
| [Host validation](../.github/workflows/ci.yml) | Commands above, on pushes, pull requests, and manual runs. | 15 minutes |
| [ARM compilation](../.github/workflows/ci.yml) | `make arm` with checksum-verified Zig 0.14.1, on the same triggers. | 10 minutes |
| [Native player](../.github/workflows/native-player.yml) | Complete patched MPlayer build and ARM verification when build inputs change, on `v*` tags, or on manual request. | 30 minutes |

Both workflows use Ubuntu 24.04, read-only repository permissions, and Node.js 24 action runtimes. Node.js is not an application dependency. These workflows validate builds but do not publish or deploy them.

CI does not establish physical CRT timing. Hardware checks must cover startup/exit, video and music, repeated overlay toggling, seeking, paused picture changes, and A/V synchronization in each supported output mode. See [tested scope](GO_DISPLAY.md#tested-scope).
