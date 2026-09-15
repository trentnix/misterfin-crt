# CRT display modes

MiSTerFin CRT can use the normal MiSTer display or a standalone interlaced menu core. Synchronization is automatic in both modes. Display settings apply when the application starts. Exit before changing modes.

## Enable or disable interlaced output

Set the `display` section in `settings.json` beside `jellyfin.conf`. The MiSTer launcher reads `/media/fat/misterfin-crt/settings.json`. An omitted section keeps the current display. The [shared example](../settings.example.json) preserves the progressive default.

To enable interlaced output:

```json
{
  "display": {
    "interlaced": true
  }
}
```

To return to the normal output on the next launch:

```json
{
  "display": {
    "interlaced": false
  }
}
```

Install the matching Go executable and MPlayer build. Also place [InterlacedMenu.rbf v0.0.1](https://github.com/iwalton3/Menu_MiSTer/releases/tag/v0.0.1) beside `jellyfin.conf`. The application verifies the supported core before changing the display. Its SHA-256 must be:

```text
0158e0338a00441271f38be0703c22253d53ea39b60a1a96b7ec964bedae8999
```

Launch MiSTerFin CRT from the normal MiSTer menu using the main `MiSTer.ini` configuration. The application loads the standalone core, enables its framebuffer, and starts the client. Exiting restores the normal menu. No replacement of `MiSTer`, `menu.rbf`, or the Linux kernel is required. Zaparoo is not required.

The application creates `Interlaced.mgl` beside its configuration and adds a marked `[MiSTerFinInterlaced]` section to `/media/fat/MiSTer.ini`. That section applies only to its named launch descriptor. Existing sections remain intact. The first change saves the original configuration to `/media/fat/misterfin-crt/MiSTer.ini.before-interlaced`. Disabling the feature leaves the isolated section available for the next use. An existing unmarked section with the same name causes a configuration error instead of being overwritten.

The core inherits the existing RGB or component settings and PAL/NTSC selection. For RGB, the scoped section enables `direct_video` and `forced_scandoubler`. For component, it enables `direct_video` without forcing the scandoubler, following the C client's display setup. Different cable/DAC combinations still require hardware testing. A failure to reach the expected full-height framebuffer cancels startup and restores the menu.

## Picture and controls

The logical UI remains 640×240 or 640×288. Browsing, photos, music, and controls keep their familiar layout. Video renders into the full 640×480 or 640×576 framebuffer rather than duplicating each decoded row. UI presentation, picture fitting, and video overlays use the full framebuffer. Letterboxing is centered within that frame. No fixed top margin or compensation for another display's overscan is applied.

Original and Zoom remain local player operations. Subtitles, Live TV captions, loading indicators, pause, seeking, and track changes retain their existing controls. The display setting does not change saved playback choices. For 480i Live TV, the client caps Jellyfin conversion at 30000/1001 fps (about 29.97) to match NTSC field timing. Progressive NTSC retains its 30 fps cap, and PAL retains 25 fps. Slower sources are not forced to the cap. Player speed and audio synchronization settings remain unchanged.

Interlaced scanout alternates fields on the CRT. It does not recover fields or detail already removed by a source or server transcode, and it does not add 50/60 fps server transcoding. Static text and thin horizontal edges can show interline flicker. The implementation retains the existing UI resolution to limit single-line detail.

## Ownership and recovery

`internal/mister/displaymode` owns core selection and supervises a child client. Before activating the framebuffer, it saves and releases graphics mode on the Linux consoles used by Main and Scripts. It waits for Main to select console 1 before pausing Main to prevent concurrent SPI access. The client owns that visible console even when its launcher retains console 2 as its controlling terminal. Physical controller input remains in the client's existing evdev path. The supervisor resumes Main and reloads the normal menu after the client exits. It restores the saved console modes and active console even after a client crash. It also terminates tagged decoder descendants after a client crash before restoring hardware ownership.

The native presenter uses the kernel VSync wait for UI frames and selects the UI framebuffer page after playback. MPlayer claims the existing output lock when its first video frame is ready, then initializes its interlaced pages. This preserves the loading animation while media opens. MPlayer composites overlays and copies each interlaced frame into the back page during its frame-completion callback, before waiting for the audio-clock presentation deadline. At the deadline, the driver only submits the hardware page flip. The kernel field counter identifies when the old front page can be reused. If the preceding flip is still pending, preparation waits briefly for the counter to advance, with a timeout. Paused redraws use the same page ownership rules. The first-frame notification remains at presentation so preparing a frame does not dismiss Loading early. Hardware acknowledgment waits have time limits. Progressive rendering keeps its existing synchronization path.

A second interlaced supervisor cannot acquire the same display lock. Invalid configuration, an unsupported core checksum, startup timeouts, and normal termination produce cleanup instead of leaving Main paused. A power loss resets the FPGA normally. Killing the supervisor itself with SIGKILL bypasses cleanup and may require restarting MiSTer.

## Validation

Tests cover configuration defaults and errors, preservation of RGB/component INI settings, repeated setup, process ownership markers, full-height picture fitting, unchanged paused-frame timestamps across Zoom changes, and consistent overlay placement. Console tests cover a Scripts console left in graphics mode, delayed keyboard discovery, activation timeouts, and restoration after a client crash. Native tests exercise the interlaced handoff, repeated composition without alpha accumulation, preparation before presentation, and page ownership during paused redraws. Field-counter tests cover an already released page, wraparound, interrupted reads, invalid reads, and a stopped display clock.

The maintainer confirmed full-height video, improved subtitle clarity, and corrected screen alignment on a consumer 4:3 CRT connected to an RGB-configured MiSTer at 640×480. Playback includes 640×480 source material, which exercises the ARM color-conversion fix. Hardware checks also verified Original/Zoom changes while paused and restoration to the normal 640×240 menu after normal and forced client exits. PAL and component output remain unverified. Extended playback testing is still limited.

## Presentation cadence

A generated 720×404 scrolling pattern reproduced occasional uneven field intervals at 30 fps without decoder drops. The standalone core’s field clock measured about 59.93 Hz. In a separate 45-second test at 30000/1001 fps, all 1,286 measured intervals between frame requests spanned two fields. The player retained its existing audio settings for both tests. The maintainer confirmed smooth Live TV with the revised frame-rate cap.

The [pinned core source](https://github.com/iwalton3/Menu_MiSTer/blob/7c51999ac103e53fa89c85fa6766a970e912d128/sys/ascal.vhd#L1734) latches the framebuffer address at the falling edge of output VSync. Initial traces used kernel VSync notifications as a proxy for scanout timing, with uncertainty near the boundary. Later traces read the kernel field counter directly without an observer thread. These measurements describe page-flip requests and field counts, not light emitted by the CRT.

A 640×480, 23.976 fps pattern compared copying after the presentation deadline with preparing the page before that deadline. Both 45-second tests recorded zero decoder drops. After excluding the first 12 seconds, the old path had seven breaks in the alternating two-field/three-field cadence across 789 intervals. The new path had none. A separate 45-second 720×404 test at the Live TV rate recorded zero decoder drops and two-field cadence for all 986 intervals after the same warm-up.

An ABC Weekend Special trace captured 2,011 decoded frames and 2,011 presentations over about 84 seconds. Incoming Jellyfin timestamps were uniformly spaced at about 41.708 ms, and all decoded frames were marked progressive. No decoder drops were observed. After excluding 12 seconds of startup settling, all 1,722 presentation intervals spanned two or three fields, with ten breaks in strict alternation. These measurements describe Jellyfin’s decoded stream, not the original media file.

The maintainer reported the same regularly uneven ABC motion in 240p and 480i, with good audio synchronization. That comparison and the absence of sustained frame loss make an interlaced-output regression less likely. The animation’s motion cadence, the source or transcode, and the normal two-field/three-field cadence of 23.976 fps playback remain possible explanations. The comparison does not establish which explanation accounts for the visible unevenness. Retain the current presentation timing unless a reproducible difference or new measurement supports another change.

### Matched Akira comparison

An unattended comparison played the same Akira segment through the shared Jellyfin client and MPlayer paths at 240p and 480i. Both runs received 2,124 frames at 720×404 and 23.976 fps, with matching timestamps and sampled luma fingerprints for every corresponding frame. No decoder drops were observed, and saved playback positions remained unchanged. After excluding 12 seconds of startup, the 240p trace contained 22 breaks in strict two/three-refresh alternation across 1,835 intervals. The 480i trace contained two. Separate active-framebuffer measurements found about 60.044 Hz for 240p and 59.930 fields/sec for 480i.

The 480i clock is closer to the approximately 59.94 fields/sec that supports exact 3:2 cadence for this stream. This sample showed steadier presentation timing in 480i, despite the maintainer perceiving slightly smoother motion in 240p. The comparison used a temporary harness without browser UI or overlays. Progressive markers followed framebuffer copies, while interlaced markers preceded hardware page-flip requests, so neither trace measures the CRT’s emitted light. Differences in scanline presentation, fine-detail flicker, and image softness remain plausible explanations for the visual impression. Retain the current timing unless another reproducible case or measurement supports a change.
