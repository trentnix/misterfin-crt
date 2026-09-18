# Build and install

Use Go 1.26.8 or later, a C compiler, and Python 3 on Linux. Go module dependencies are pinned in [go.mod](../go.mod). MiSTer builds also need an ARM cross-compiler. Docker builds the separate MPlayer executable on the development machine, not on MiSTer.

## Go client

```sh
make host
make arm
# If Zig is outside PATH:
ZIG=/absolute/path/to/zig make arm
```

The outputs are `build/mistervision` and `build/mistervision-arm`. The ARM target enables cgo and uses `GOOS=linux GOARCH=arm GOARM=7`. The [compiler wrapper](../tools/zig-cc-go.sh) targets `arm-linux-gnueabihf.2.31` and Cortex-A9. Zig 0.14.1 has been tested. `GO_ARM_CC` can select another compatible compiler. Set `GOCACHE` and `ZIG_GLOBAL_CACHE_DIR` if their default directories are unwritable.

Development builds show `dev`, the Git revision, and a modified marker when available. Jellyfin and Plex requests report the same version label, without the revision suffix. Set `VERSION` for a stable release label:

```sh
make arm VERSION=v1.2.0
```

## MPlayer

Build the matching patched player from [Dockerfile.mistervision](../docker/Dockerfile.mistervision):

```sh
make native-player
```

The outputs are `build/mistervision-mplayer-arm` and its source/compiler record, `build/mistervision-mplayer-build.txt`. The base image is pinned by digest, and the build verifies the MPlayer source archive with SHA-256. The Bullseye toolchain targets MiSTer's glibc 2.31. The patches provide shared overlays, picture changes, captions, interlaced presentation, and playback timing fixes.

The original C client's player cannot substitute for this build. Update both binaries together when their protocol changes. See [third-party notices](THIRD_PARTY.md) for corresponding source and licenses.

The native build also exports `build/mistervision-mplayer-source.tar.xz`, the verified upstream source used by that build. The source archive includes MPlayer's bundled FFmpeg. The Go vulnerability scan does not audit these native dependencies.

## Release bundles

From a clean Git checkout, build a release with:

```sh
make release VERSION=v1.2.0
```

This command rebuilds both ARM executables, records their metadata and checksums, and packages them under `build/releases/v1.2.0/`. It requires the same Go, Zig, Python, and Docker tools as the individual builds. Stable `vMAJOR.MINOR.PATCH` versions are required. Dirty checkouts, untracked source files, invalid binaries, source checksum mismatches, and existing output directories stop the build. Failed builds do not publish a partial bundle.

| Artifact | Contents |
| --- | --- |
| `mistervision-v1.2.0-mister.zip` | SD card layout with both binaries, Scripts launcher, configuration examples, installation instructions, version/build metadata, component notices, and checksums. |
| `mistervision-v1.2.0-source.tar.gz` | Committed project source plus the exact upstream MPlayer archive. Patches and build recipes remain under `docker/`. |
| `SHA256SUMS` | Checksums for both downloadable archives. |

The ZIP contains only example configuration files. It contains no active `jellyfin.conf`, `settings.json`, sign-in, preferences, or caches. Read its `INSTALL.txt` before copying files. The optional interlaced core remains a separate download.

To rebuild MPlayer from the source bundle, run `make native-player` in its extracted project directory. Docker uses the included upstream archive and still verifies its checksum. The base image and compiler packages need network access or a local Docker cache.

`make release-manifest` can run after separate `make arm` and `make native-player` builds. It writes `build/release-manifest.txt`, which records Go metadata, MPlayer source/compiler details, and both executable checksums. Packaging includes that record as `mistervision/BUILD.txt`, with the release version and source revision. Packaging the same inputs produces identical archives. This does not promise identical compiler output across toolchain or environment changes.

The [release workflow](../.github/workflows/release.yml) runs when a version tag is pushed. It can also run manually with that tag selected as the workflow ref. It builds the bundle and creates a GitHub draft release with generated notes and all three assets. It refuses to overwrite an existing release.

Before publishing, review the notes, require successful Go validation, verify the downloaded checksums, and test the paired binaries on MiSTer. Publishing requires a manual action on GitHub. Draft or private releases are unavailable to the application's unauthenticated checker.

The [latest release](https://github.com/trentnix/mistervision/releases/latest) provides both archives and their checksums. Bundles include `mistervision/UPDATE_FORMAT` with transaction format `1`. The updater rejects older or incompatible formats before replacing any files.

## Install on MiSTer

Copy these files to the SD card and make them executable:

| File | Destination |
| --- | --- |
| `build/mistervision-arm` | `/media/fat/mistervision/mistervision` |
| `build/mistervision-mplayer-arm` | `/media/fat/mistervision/mplayer-arm` |
| [`tools/mistervision.sh`](../tools/mistervision.sh) | `/media/fat/Scripts/MiSTerVision.sh` |

Jellyfin discovery and Plex account-based server selection need no connection file. Choose Plex under About → Connections. For an explicit Jellyfin or Plex address, copy `settings.example.json` to `settings.json` beside the binaries and set `server.provider` and `server.url`. See [configuration and migration](GO_CONFIGURATION.md) for existing installations. Launch **MiSTerVision** from Scripts so Main_MiSTer enables framebuffer output. Launcher filenames must contain no spaces. A direct progressive-mode launch over SSH does not enable framebuffer output through Scripts.

The launcher enables both CPU cores, hides the console cursor, and reloads the normal menu after a successful exit. Failures leave their messages visible. Login and playback choices persist under `/media/fat/mistervision/state`. Caches use separate [artwork directories](GO_BROWSING.md#persistent-artwork-cache). For 480i, follow the [display guide](GO_DISPLAY.md).

For manual installation, exit before replacing binaries. Copy replacements to temporary filenames in the installation directory, set executable permissions, then rename them over the installed files. Always replace the client and matching player together.

## Moving from MiSTerFin CRT

The rename changes binaries, install directories, launcher names, release assets, and environment variables. Install the new application and its matching MPlayer together. MiSTerFin CRT v1.0.x cannot install the renamed bundle through About. MiSTerVision releases support subsequent updates from About.

On MiSTer, exit the old app and back up `/media/fat/misterfin-crt`. Copy its `settings.json`, optional `jellyfin.conf`, `state`, `covercache`, `gridcache`, `InterlacedMenu.rbf`, and custom assets into `/media/fat/mistervision`. Copy only files that exist. Update absolute paths in settings, including custom backgrounds and music assets.

Then install the new binaries and `Scripts/MiSTerVision.sh`. After testing, remove `Scripts/MiSTerFin-CRT.sh` so the menu has one entry. Keep the backup until the new installation is verified.

On desktop, move or copy `$XDG_CONFIG_HOME/misterfin-crt` to `$XDG_CONFIG_HOME/mistervision`, using `~/.config` when `XDG_CONFIG_HOME` is unset. Preserve both providers’ sessions and playback preferences. The same directory rename applies beneath the user cache root. Do not overwrite an existing destination without reconciling its contents. Explicit `--state-dir` paths remain supported, so development commands can continue using an old directory intentionally.

Environment overrides now start with `MISTERVISION_`, for example `MISTERVISION_SETTINGS` and `MISTERVISION_CACHE_ROOT`. The internal player and launcher protocol changed with the name, so an old player must not be paired with the renamed client. The new interlaced section is `[MiSTerVisionInterlaced]`. The old managed section can remain inert until removed after verification.

[Settings migration](GO_CONFIGURATION.md#migration) still combines legacy JSON configuration files. It does not move installation directories.

## Application updates

In About, select **View release**, review the notes, then select **Install**. Automatic installation requires the client at `/media/fat/mistervision/mistervision` and the configured player at `/media/fat/mistervision/mplayer-arm`. The standard Scripts launcher is updated with the pair. Custom installations and desktop development retain manual installation.

For manual upgrades, use the latest release ZIP. Keep the existing settings and state files. Do not copy example configuration over active configuration.

Downloads use verified HTTPS from this repository's GitHub release assets without credentials. The installer verifies the outer SHA-256 checksum, every bundled file, the release version, transaction format, and ARM executable headers. File counts and sizes are bounded. Checksums detect damaged downloads. They are not signatures independent of GitHub.

The SD card must have room for the download, staged files, rollback copies, and a temporary replacement file. The installer downloads, validates, and backs up everything before changing installed files. Storage or validation failures leave the installation intact. Settings, credentials, preferences, artwork caches, and the separately installed 480i core are excluded from replacement.

Update failures distinguish download, verification, and storage problems and explain what to try next. If a release requires manual installation, Install is disabled for that release and the page directs you to its ZIP. Cancellation and recoverable failures confirm that the existing installation was kept.

A successful update shows “Update installed. Restarting...” for two seconds, closes the client, and starts the installed launcher again. In 480i, the supervisor restores the normal core before restart. Cleanup or startup failures stop with an error instead of retrying. Cancellation or a replacement failure restores the old files. Custom launchers without restart support require reopening the app after an update.

If interrupted, startup uses `.update-pending` to finish rollback, then re-executes the restored client before opening the display. A committed transaction only needs backup cleanup. Do not delete pending recovery files. If recovery cannot finish, the app stops before playback. Correct the storage problem and relaunch, or manually reinstall the matching pair while preserving settings and state.

## Local development

```sh
python3 tools/ghostty/ghostty_harness.py --demo --ntsc
```

The demo builds the client and serves mock browsing data. It does not provide playable media. See the [harness guide](../tools/ghostty/README.md) for dependencies and real-server playback.

For a framebuffer test without a media server:

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

`make test` covers Go with and without cgo plus Python/native adapter tests. `make test-browse` runs the built client against isolated HTTP/WebSocket fixtures. Generated media and local servers avoid a real media-server account or MiSTer dependency. Decoder tests can skip when their external dependencies are absent.

| CI job | Checks and triggers | Timeout |
| --- | --- | --- |
| [Host validation](../.github/workflows/ci.yml) | Commands above, on pushes, pull requests, and manual runs. | 15 minutes |
| [ARM compilation](../.github/workflows/ci.yml) | `make arm` with checksum-verified Zig 0.14.1, on the same triggers. | 10 minutes |
| [Native player](../.github/workflows/native-player.yml) | Complete patched MPlayer build and ARM verification when build inputs change, on `v*` tags, or on manual request. | 30 minutes |

The validation workflows use Ubuntu 24.04, read-only repository permissions, and Node.js 24 action runtimes. Node.js is not an application dependency. The separate release workflow has a 40-minute limit and repository write permission to create draft releases. No workflow deploys to MiSTer or makes the repository public.

CI does not establish physical CRT timing. Hardware checks must cover startup/exit, video and music, repeated overlay toggling, seeking, paused picture changes, and A/V synchronization in each supported output mode. See [tested scope](GO_DISPLAY.md#tested-scope).
