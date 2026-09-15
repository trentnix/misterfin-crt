# CRT display modes

The default keeps MiSTer's current display, normally 240p for NTSC or 288p for PAL. Optional interlaced output uses a standalone Menu core at 480i or 576i. Synchronization is automatic. Exit the application before changing settings.

## Enable or disable interlaced output

Install the matching [Go client and MPlayer](GO_BUILD.md), then place [InterlacedMenu.rbf v0.0.1](https://github.com/iwalton3/Menu_MiSTer/releases/tag/v0.0.1) beside `jellyfin.conf`, normally in `/media/fat/misterfin-crt`. The supported core must have this SHA-256:

```text
0158e0338a00441271f38be0703c22253d53ea39b60a1a96b7ec964bedae8999
```

Set the `display` section in `settings.json`:

```json
{"display": {"interlaced": true}}
```

Launch **MiSTerFin-CRT** from the normal Scripts menu using the main `MiSTer.ini`. The client verifies and loads the core, then restores the normal menu on exit. No replacement of `MiSTer`, `menu.rbf`, or the kernel is part of this setup. Zaparoo is not required.

To return to progressive output, set `interlaced` to `false` or omit the section. Preserve other settings when editing. The same launcher supports both modes. Separate launchers such as **MiSTerFin-CRT-480i** simply select a different configuration directory.

## Configuration changes and recovery

The application writes `Interlaced.mgl` beside `jellyfin.conf` and a marked `[MiSTerFinInterlaced]` section in `/media/fat/MiSTer.ini`. Before its first change, it saves `/media/fat/misterfin-crt/MiSTer.ini.before-interlaced`. Existing sections remain intact. An unmarked section with the same name causes an error instead of being overwritten. Disabling interlacing leaves the isolated section available for later use.

The scoped section inherits RGB/component and PAL/NTSC settings. RGB enables `direct_video` and `forced_scandoubler`. Component enables `direct_video` without forcing the scandoubler. These rules do not establish compatibility with every cable or DAC.

The supervisor waits for the expected framebuffer, manages console modes, and pauses Main while the child owns hardware. It stops orphaned decoders, restores consoles, resumes Main, and reloads the normal menu after normal exit, handled termination, or child failure. A display lock prevents two supervisors. SIGKILL of the supervisor bypasses cleanup and can require restarting MiSTer. [Supervisor logs](GO_DIAGNOSTICS.md) help diagnose failed handoffs.

Native output requires a kernel with working framebuffer mapping and VSync support. Early MiSTer Linux 6.18 builds omitted framebuffer callbacks and failed with `mmap framebuffer: No such device`. That failure requires the [kernel callback fix](https://github.com/MiSTer-devel/Linux-Kernel_MiSTer/commit/ea2212221ad137cf26bf5caa7ad3dab7216435a6), not an application setting.

## Picture and timing

The UI layout remains 640×240 or 640×288. Interlaced video uses all 640×480 or 640×576 pixels. Letterboxing and shared overlays are centered in that full framebuffer. Original/Zoom, subtitles, captions, pause, and seeking retain their normal controls.

MPlayer prepares a back page before its presentation deadline, then submits the flip at that deadline. A kernel field counter prevents reuse while a flip is pending. Paused redraws use the same ownership rules. The loading indicator clears on the first presented frame, not while a frame is merely being prepared.

For 480i Live TV, Jellyfin conversion is capped at 30000/1001 fps. Progressive NTSC uses 30 fps and PAL uses 25 fps. Slower sources are not forced to those rates. The player retains audio-clock correction and does not force playback speed. Interlaced output does not recover source fields lost during conversion or add 50/60 fps transcoding. Film-rate material can retain normal 3:2 cadence, and thin detail can show interline flicker.

## Tested scope

I test 240p and 480i on a consumer 4:3 CRT, using a MiSTer configured for RGB through its 9-pin output and a Retrovision YPbPr cable. This is not validation of MiSTer's direct YPbPr mode. Browsing, playback, shared overlays, paused picture changes, and menu restoration have been checked on that setup. Jellyfin testing used version 12.

Generated patterns and matched media comparisons found no sustained decoder drops after the timing fixes. Field-counter measurements describe presentation requests, not light emitted by the CRT. Perceived judder still depends on source cadence and display mode. Preserve the current timing unless a reproducible case supports a change.

PAL/576i and direct YPbPr validation remain deferred because suitable hardware is unavailable. Zaparoo DDR integration is also deferred. The original C project's reports for SCART, professional monitors, VGA, and HDMI do not establish support in this client.
