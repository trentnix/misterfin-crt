# Go framebuffer prototype

For the subsequent Ghostty browsing milestone, see [GO_BROWSING.md](GO_BROWSING.md). The sections below record the initial framebuffer milestone.

Host rendering, ARM cross-compilation, and hardware framebuffer drawing now work. On September 12, 2026, the Go build passed framebuffer drawing and restoration checks on a MiSTer with a rebuilt kernel. Direct CRT confirmation and authenticated browser/playback checks remain pending. The C baseline is unchanged.

## Build and run on Linux

The prototype uses Go 1.26 and a C compiler. Validation used Go 1.26.4 on Linux amd64. There are no external Go module dependencies.

```sh
make -f Makefile.port host
make -f Makefile.port test
make -f Makefile.port headless
```

The headless target writes `build/go-frame.raw` and `build/go-frame.png`. The image contains eight color bars, a grayscale ramp, and a white perimeter. The raw frame uses the inherited harness's BGRX8888 format and `tools/raw_to_png.py`. The inherited interactive `tools/run-local.sh` still launches the C application.

To build and display the Go test frame inside Ghostty, run:

```sh
python3 tools/ghostty/ghostty_harness.py --go --ntsc
```

Use `--go --pal` for PAL. Press Ctrl+C to exit. The harness supplies `-wait` so the frame remains visible until interrupted. Use `--browse` or `--demo` for the subsequent Go browser. Without a Go mode flag, the Ghostty harness continues to run the C client with its existing controls.

The existing environment variables also work:

```sh
MISTERFIN_FB=640x240 MISTERFIN_FRAME_OUT=build/go-ntsc.raw ./build/misterfin-go
python3 tools/raw_to_png.py build/go-ntsc.raw 640 240 build/go-ntsc.png
./build/misterfin-go -headless 1280x720 -output build/go-hdmi.raw
python3 tools/raw_to_png.py build/go-hdmi.raw 1280 720 build/go-hdmi.png
```

Flags override the environment. No Jellyfin configuration, assets, or external player are needed. `-hold 10s` keeps the process alive for ten seconds. `-wait` waits until interrupted and cannot be combined with a nonzero hold time. SIGINT and SIGTERM trigger normal cleanup. The default hold time is zero, which suits headless capture. Hardware runs must specify a hold time or `-wait` to leave the frame visible for inspection.

## Cross-compile for MiSTer

Validation used Zig 0.14.1 and the C baseline's target, `arm-linux-gnueabihf.2.31 -mcpu=cortex_a9`. The wrapper passes cgo's compiler and linker arguments to Zig unchanged.

```sh
make -f Makefile.port arm
# If Zig is outside PATH:
ZIG=/absolute/path/to/zig make -f Makefile.port arm
file build/misterfin-go-arm
readelf -l build/misterfin-go-arm
readelf --version-info build/misterfin-go-arm
```

The target sets `CGO_ENABLED=1 GOOS=linux GOARCH=arm GOARM=7`. `GO_ARM_CC` can select another compatible C cross-compiler. The verified artifact is an ARM EABI5 executable with Cortex-A9 attributes. It requests `/lib/ld-linux-armhf.so.3` and imports `GLIBC_2.4` symbols from `libc.so.6` and `libpthread.so.0`. Cross-compilation establishes build compatibility only. It does not establish that the Go runtime or framebuffer works on a particular MiSTer.

Go 1.26 requires Linux 3.2 or later, according to the [Go minimum requirements](https://go.dev/wiki/MinimumRequirements). The ELF note printed by `file` is not the Go runtime's minimum kernel requirement.

If the build environment restricts cache writes, set `GOCACHE` and `ZIG_GLOBAL_CACHE_DIR` to writable directories. The local validation used `/tmp/misterfin-go-cache` and `/tmp/misterfin-go-zig-cache`.

## Hardware validation

### September 12 retest with rebuilt kernel

The user supplied a kernel build containing the framebuffer fix and confirmed that the installed C MiSTerFin works again. SSH reports `Linux MiSTer 6.18.38-MiSTer #1 SMP Sat Sep 12 11:53:48 UTC 2026 armv7l GNU/Linux`. The framebuffer remains 640×240 at 32 bits per pixel.

The current Go source cross-compiled successfully and was copied only to `/tmp/misterfin-go-arm`. Hardware execution completed with status 0 and reported logical and output dimensions of 640×240. A framebuffer capture showed the expected color bars, white perimeter, and grayscale ramp. The first capture differed from the host reference by 384 bytes in the console cursor rectangle at x=0–7, y=160–175. The initial restoration comparison also differed within that cursor rectangle.

With the tty2 cursor temporarily hidden, full 614400-byte framebuffer comparisons passed after timed exit, SIGINT, and SIGTERM. Each run exited with status 0. These checks establish mapping, drawing, and memory restoration. They do not establish CRT scanout quality or timing.

The browser launched against the installed Jellyfin configuration with its own temporary state directory, `/tmp/misterfin-go-state`. A hardware capture confirmed the Quick Connect screen. Quick Connect sign-in succeeded. The user reported that the SSH-launched browser was not visible on the CRT. Main_MiSTer enables framebuffer scanout when launching from its Scripts menu, a step bypassed by the SSH test. A separate `MiSTerFin-Go-Test.sh` launcher was prepared for that path. Visible browser output and playback remain unverified. The first Scripts launch failed with `no such device or address`: terminal input opened `/dev/tty`, which requires a controlling terminal. The failure was reproduced on MiSTer with `setsid` and terminal stdin. The input reader now falls back to reopening stdin when `/dev/tty` returns `ENXIO`, still requiring terminal ioctls to succeed. The fallback owns its descriptor and nonblocking flags. A regression test exercises navigation and exit with terminal stdin and no controlling terminal. No desktop credentials were transferred, and the installed C client, player, launcher, kernel, and core were not replaced during this retest.

### Initial validation before the kernel fix

Hardware was reached at `root@192.168.1.42` after `mister.local` failed to resolve. The device runs Linux `6.18.38-MiSTer`, built September 7, 2026, on a dual-core ARM Cortex-A9 with VFPv3 and NEON. GNU libc reports version 2.31, and `/lib/ld-linux-armhf.so.3` points to `ld-2.31.so`. The framebuffer reports 640×240 at 32 bits per pixel, stride 2560, and 614400 bytes of memory at physical address `0x22001000`.

The ARM binary was copied to `/tmp/misterfin-go-arm`. It ran successfully in headless mode on the device. Its 640×240 raw output matched the host output byte for byte, with SHA-256 `bd3cc032d928f432ff1b49ab173289a91bfec78f310a52d0b72ffc1b53a87a01`.

The hardware run failed before writing any pixels. Framebuffer open and geometry ioctls succeed, but `mmap` returns `ENODEV`. A standalone C diagnostic reproduced the same failure. The kernel logged `fb0: fb_WARN_ON_ONCE(!info->fbops->fb_mmap)`. Reading the framebuffer with `dd` also failed because the read callback is absent, so the attempted before/after checksums did not validate restoration.

The installed C client was also tested through `/media/fat/Scripts/MiSTerFin.sh` over SSH, after checking that no client, player, or pending updater was active. The binary is dated September 1, is 1885176 bytes, and has SHA-256 `18f18b352ec4866e79e8eac5384c0416f8ee486c3f188e30e4104ffc4b90655d`. The launcher exited with status 1 and printed `mmap framebuffer: No such device` followed by `Cannot open /dev/fb0`. The binary and launcher were not replaced. A launch from the physical Scripts menu remains unverified. That path can switch to tty2 and enable framebuffer display before running the script, so the SSH launch alone does not establish identical launch conditions.

The upstream [September 8 framebuffer fix](https://github.com/MiSTer-devel/Linux-Kernel_MiSTer/commit/ea2212221ad137cf26bf5caa7ad3dab7216435a6) adds the missing callbacks and describes this exact failure. The installed kernel predates that fix. A kernel build containing the fix is required before repeating the visible-output and restoration checks. No kernel, core, or persistent MiSTer configuration was changed during validation.

For future device checks, collect the kernel, CPU, libc, and loader details:

```sh
uname -a
cat /proc/cpuinfo
/lib/libc.so.6 | head -1
ls -l /lib/ld-linux-armhf.so.3
cat /sys/class/graphics/fb0/virtual_size
cat /sys/class/graphics/fb0/bits_per_pixel
```

After the target is confirmed, copy only `build/misterfin-go-arm` to `/tmp/misterfin-go-arm` on the device. Run the following command from a MiSTer terminal with the framebuffer available and the C client and player stopped:

```sh
/tmp/misterfin-go-arm -device /dev/fb0 -hold 10s
```

Confirm the color order, white perimeter, grayscale ramp, centered geometry, and restoration after the ten-second timeout. Repeat with SIGINT and SIGTERM. Record the device's output mode and reported logical/output dimensions. The adapter reports ioctl, mapping, and unsupported-layout errors instead of treating them as successful presentation. Confirm the frame visually because a successful ioctl and memory copy cannot establish visible output or timing.

The prototype does not install a launcher, change a core, use `/dev/mem`, stop `Main_MiSTer`, or invoke the C updater. The separate executable and temporary hardware path permit testing alongside the C client. The display clears the framebuffer and console text on open and close. It does not restore a snapshot of the startup screen. Cleanup cannot run after SIGKILL, a crash, or power loss.

## Launch the Go browser from the Scripts menu

For CRT testing, copy [`tools/misterfin-go-test.sh`](../tools/misterfin-go-test.sh) to `/media/fat/Scripts/MiSTerFin-Go-Test.sh` the ARM build to `/tmp/misterfin-go-arm`, and the [Go-specific MPlayer build](GO_PLAYBACK.md#mister-use-and-remaining-work) to `/tmp/misterfin-go-mplayer-arm`. Both binaries must be executable. Launch **MiSTerFin-Go-Test** from the MiSTer Scripts menu. That path switches to tty2 and enables framebuffer output through Main_MiSTer. An SSH launch alone does not perform that setup.

The test launcher uses the installed Jellyfin configuration, the temporary Go-specific player, and a separate Go session at `/tmp/misterfin-go-state`. The C player does not read Go overlays and must not be used for this test. Both binaries and the session are temporary and must be prepared again after reboot. Use a keyboard: arrows to navigate, Enter to select, Escape to go back, and Q to quit. The launcher removes Main_MiSTer’s inherited CPU-1-only affinity with `taskset -p 3` before starting Go. Both the UI and decoder can then use both Cortex-A9 cores. The launcher clears the active virtual console directly at startup and exit. It hides the console cursor while running and restores it on exit. It does not replace the C launcher or client.

## Platform contract and audit

`internal/platform.Display` is a pure-Go interface. `Geometry` reports logical input and physical output dimensions. `Present` accepts exactly `Width * Height * 4` tightly packed BGRX bytes. Go owns the slice. C borrows the pointer synchronously and retains no Go memory or callbacks. C owns its mapped or allocated output buffer and the hardware snapshot. Callers must serialize calls and invoke `Close`, which is idempotent. Errors propagate to Go and the command exits nonzero on failure.

The adapter derives geometry and presentation from `src/fb.c`, with the original attribution and license retained. It uses Linux fbdev and `FBIO_WAITFORVSYNC`, then copies or scales the complete frame in one cgo presentation call. Hardware uses the existing mode without changing framebuffer settings. Unsupported pixel formats, nonzero viewport offsets, invalid stride, and insufficient framebuffer memory are rejected. Dimensions and allocations are bounded.

PAL and NTSC retain native geometry. Physical 480/576-line modes double logical rows. Larger and widescreen canvases use the baseline's centered 4:3 UI scaling. Headless mode deliberately applies the same 480/576-line doubling as hardware. The inherited C headless implementation does not simulate that behavior. Headless dumps contain physical output dimensions.

The input audit found that `src/input.c` combines global evdev state, terminal state, scripted input, held-button queries, and the `g_running` lifecycle flag. That API is not exposed through cgo in this milestone. The prototype accepts no controller events and uses duration or process signals for shutdown. A future input boundary must return value events without C callbacks into Go and define press, release, repeat, disconnect, and shutdown behavior explicitly.

DDR, raw SPI page flipping, interlaced playback compensation, and player handoff remain outside this adapter. The milestone validates standard framebuffer presentation only. Hardware timing and DDR correctness require separate device tests.

## Validation record

- Host build, raw capture, PNG conversion, and visual inspection passed.
- The inherited C host build passed with existing compiler warnings.
- ARM cgo cross-build passed with Go 1.26.4 and Zig 0.14.1.
- Go tests passed with cgo enabled and disabled. Adapter tests check every output pixel for PAL, NTSC, line doubling, HDMI scaling, and letterboxing. They also cover buffer reuse, invalid input, failed dumps, and close behavior.
- `go vet ./...` and `GOEXPERIMENT=cgocheck2 go test ./internal/platform` passed.
- Headless SIGINT and SIGTERM checks exited successfully and produced complete NTSC frames.
- All 16 Ghostty harness tests passed after adding `--go`. PAL and NTSC pseudoterminal checks verified complete Kitty image uploads, Ctrl+C exit, image deletion, and terminal restoration. The user confirmed that the test frame looks correct in Ghostty.
- The inherited `make test` passed through authentication and pause UI after allowing its localhost HTTP server outside the network sandbox. It stopped at `tests/test_sfx.c:57`, whose assertion requires a host without `libasound`. This desktop has ALSA. The remaining hero, cache sweep, and 13 Ghostty tests passed when run separately. The baseline test was not changed.
- Initial physical MiSTer execution and headless output passed on Linux `6.18.38-MiSTer` with glibc 2.31. The September 12 rebuilt-kernel retest also passed hardware framebuffer drawing and restoration after timeout, SIGINT, and SIGTERM. Direct CRT confirmation and authenticated browser/playback checks remain pending.

The `c-baseline` tag still points to `19d99fa5f479692e45ea7b5dddc42e42fb1782a9`.
