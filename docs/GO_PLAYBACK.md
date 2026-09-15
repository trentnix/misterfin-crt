# Go media playback

The Go client can start and resume movies, episodes, videos, and music videos through Jellyfin's progressive MPEG-2/MP3 transcode endpoint. Desktop playback opens FFplay in a separate window by default, with optional video inside Ghostty through libmpv. The MiSTer path launches the patched `mplayer-arm` executable and passes Go-rendered overlays to its framebuffer output driver while MPlayer owns `/dev/fb0`. Live TV channels use the C client’s negotiated stream setup. The C application remains unchanged.

## Current usage

In my current workflow, the players serve these roles:

- **MPlayer:** Everyday playback on MiSTer through the patched `mplayer-arm` executable, with CRT framebuffer output and shared UX overlays.
- **Python/libmpv:** Local development and playback testing inside Ghostty with `--inline-video`. Python controls libmpv, which decodes the media. FFplay is not involved in this mode.
- **FFplay:** Primarily an alternate test player, including automated decoding tests. It displays video in a separate desktop window.

The harness still selects FFplay for video when `--inline-video` is omitted. FFplay remains available for interactive use, but it is not required for my usual Ghostty or MiSTer playback. See [Video inside Ghostty](#video-inside-ghostty) for the usual local playback command and [Desktop use](#desktop-use) for the FFplay alternative.

## Playback controls

These are the default bindings. [Input configuration](GO_INPUT.md) supports per-device controller layouts, button overrides, analog axis mappings, and custom button names. Playback overlays show key badges for the active input device, omit unbound actions, and wrap long labels. Music keeps its track and seek controls on a separate row from pause and stop.

| Action | Xbox controller | Keyboard |
| --- | --- | --- |
| Show or hide the menu | Any D-pad direction | Any arrow key |
| Seek backward or forward | LT / RT | J / L |
| Previous or next music track | LB / RB | [ / ] or Page Up / Page Down |
| Pause or resume | B | Enter or B |
| Stop and return | A | Escape or A |

Menu toggles and track changes act once per press. Triggers repeat seeking after a 350 ms hold, then every 250 ms. Video seeks retain 30-second steps, the destination preview, and the 0.5-second delay after the last seek action. Music seeks move 10 seconds within the existing player and preserve pause state. Live TV does not seek. Shoulder buttons have no action during video. Browsing and photo navigation keep their directional controls.

Supporting terminals use [Kitty keyboard event reporting](https://sw.kovidgoyal.net/kitty/keyboard-protocol/) to distinguish presses from repeats and releases. Ghostty therefore keeps menu toggles and track changes from repeating while a key is held. Other terminals retain legacy input, which cannot distinguish repeated presses from a held key. The application restores the terminal's keyboard mode when it exits.

## Desktop use

FFplay must be installed and available as `ffplay` on `PATH`. Run the browser against a real Jellyfin server:

```sh
python3 tools/ghostty/ghostty_harness.py --browse --ntsc --config jellyfin.conf
```

Use `--pal` for PAL. Open a movie's details, then press B or Enter to play. If Jellyfin has an unwatched resume position, playback starts there. During playback, B or Enter in Ghostty pauses or resumes without adding instructions. Any arrow toggles the controls in Ghostty. The menu expires after three seconds. A stops playback and returns to details. Any keypress in the FFplay window closes the video window. Q or Ctrl+C in Ghostty stops playback and exits the browser.

With the default FFplay mode, video appears in its own window and Ghostty shows the title and artwork. Any arrow toggles the controls in Ghostty. Keep keyboard focus in Ghostty when using those controls. The inherited mock-server demo supplies browsing data and artwork but does not serve playable videos. Automated decoding tests use a generated video fixture instead.

## Video inside Ghostty

The host must have Python 3 and a shared libmpv library. This workstation already has `libmpv.so.2` version 0.37. Check that the helper can load the API, then launch the browser:

```sh
python3 tools/ghostty/video_player.py --check
python3 tools/ghostty/ghostty_harness.py --browse --ntsc --inline-video --config jellyfin.conf
```

Open a video's details and press B or Enter. Video replaces the browser image inside Ghostty. Audio plays through the desktop audio output.

B or Enter pauses or resumes without a pause overlay. Any arrow toggles the title, playback time, and controls over the video. The menu expires after three seconds. Pausing or resuming hides the menu immediately. A stops playback and restores the details screen. Q exits.

The inline mode defaults to 60 terminal presentations per second. Use `--fps 25` to select a lower limit. The mock demo does not supply playable media.

The Python helper uses libmpv's [software rendering API](https://github.com/mpv-player/mpv/blob/v0.37.0/libmpv/render.h). libmpv handles decoding, audio, and presentation timing. A dedicated render thread writes complete BGRX frames atomically to a separate decoder frame file, using the output path with `.video` appended. Rendering at 640×480 before sampling PAL or NTSC rows preserves the harness's physical 4:3 aspect ratio and source letterboxing.

The browser produces one straight-alpha BGRA overlay without knowing which video output is active. The headless output backend reads each clean decoder frame, composites the overlay, and presents the result. The menu never modifies the decoder frame, so hiding or expiring it restores the clean picture even while paused.

The frame-file backend wakes on decoder frame publication, and the terminal presenter wakes when Go completes the composed frame. The terminal presenter defaults to a 60 Hz cap, counts upload time toward each interval, and skips expired slots after a stall. Terminal presentation can still drop frames if uploads cannot keep up. Visible smoothness and audible synchronization still need interactive Ghostty validation.

The Go process retains stream ownership and session reporting. The helper receives media on descriptor 3, reads pause commands from a separate standard-input pipe, and returns numeric playback positions. The direct Go binary accepts `-terminal-player tools/ghostty/video_player.py` with `-browse`, a 640×240 or 640×288 `-headless` geometry, and `-output`. The terminal helper cannot be combined with `-player`. MiSTer does not need Python or libmpv.

## Photos and music

Photos open full screen with no title or instructions over the image. Left and Right move between photos in the current folder, skip other item types, and fetch additional pages as needed. Up reveals the title, folder position, and navigation controls for three seconds. A returns to the folder with the current photo selected. R retries a failed image request. Photos remain manual, matching the C viewer. Slideshows and zoom remain pending.

The photo request uses Jellyfin's primary image at the logical framebuffer dimensions with quality 90, matching the C endpoint. The viewer preserves the source aspect ratio on the physical 4:3 display, including portrait letterboxing. Loading is cancellable and shares the bounded artwork cache.

Browse Music → artist → album, then select a track with B or Enter to start playback. Tracks advance automatically in list order, including across pages. The queue stops at the end of the album or at a non-audio item. A stops playback and returns to the track list with the current track selected. Q exits.

B or Enter pauses or resumes music without adding a pause overlay or instructions. Any direction toggles the controls for three seconds. LB/RB or brackets select the previous or next track immediately, whether the menu is visible or hidden. LT/RT or J/L seek within the track. B or Enter hides the controls immediately. The clean-pause behavior follows commit `bb31e83` and the C pause UI (`src/pause_ui.c` in the C integration repository at `19d99fa5f479692e45ea7b5dddc42e42fb1782a9`).

Music uses the C client's `/Audio/{id}/stream?static=true` request, with a unique play session ID. It streams the original audio and reports `DirectStream`, including pause state and playback positions. Each newly selected track starts at the beginning. Session progress and completion use the existing reporting and user-data endpoints.

The Ghostty harness uses libmpv for controllable audio in both video modes. The decoder reads from a private loopback HTTP address. Go forwards byte-range requests to the fixed Jellyfin audio URL, which keeps the original stream seekable without exposing server credentials to the player. The adapter closes when the track ends or playback is canceled. The direct Go binary accepts `-audio-player tools/ghostty/video_player.py` for the same controls. Without an audio helper, direct headless use retains the FFplay fallback with pause control. That fallback cannot seek audio and shows a notice if seeking is requested. On MiSTer, MPlayer uses the same audio adapter, native slave commands, and the C volume reduction and 48 kHz resampling filter.

Shuffle, repeat modes, and visualizers remain pending. Photo and music controls are covered by desktop tests. The preceding checks are the original milestone validation. Subsequent maintainer testing confirmed CRT playback and overlays. See GO_TRACKS.md for the newer track-selection validation boundary.

On a resumable library video’s details screen, B or Enter resumes the saved position. SELECT (Tab in Ghostty or View/Back on an Xbox controller on MiSTer) starts playback from the beginning. The restart hint appears only for an unwatched video with a saved position. Restart uses an explicit zero offset even if Jellyfin returns a saved resume position during startup.

## Subtitle and audio-track selection

Press SELECT/Tab during recorded-video playback to open the Subtitles/Audio picker. Text subtitles render through the shared overlay on MiSTer and inline Ghostty. Audio changes and image-subtitle selection replace the stream at the current position while preserving pause state. See [GO_TRACKS.md](GO_TRACKS.md) for controls, timing adjustment, and the separate FFplay window fallback.

## Video seeking

During movie, episode, video, or music-video playback, LT/RT or J/L seek backward or forward by 30 seconds. When the playback menu is hidden, two quick presses reveal a compact overlay showing the destination time. When seeking starts with the menu open, the destination and loading status stay in that menu through retargeting and stream handoff. The menu stays visible until playback starts again, then its three-second timeout restarts. Further forward-seek presses add 30 seconds to that destination, and backward-seek presses subtract 30 seconds. Repeated presses accumulate, and the client waits 0.5 seconds after the last press before preparing the replacement stream. At that point, the current decoder pauses so playback does not continue beneath the “Seeking…” overlay. If LT/RT or J/L is pressed while that replacement is loading, the client cancels it, restores the updated destination-time overlay for another 0.5 seconds, and then prepares the new replacement under the “Seeking…” overlay. The overlay changes to “Loading…” when the replacement player starts. The last decoded frame remains visible beneath each overlay. The replacement resumes automatically unless playback was already paused before seeking. Targets stay between the beginning and one second before the known end. Seeking is available after the first playback position arrives. Live TV does not seek. Music uses the direct ten-second seeking path described above.

The client requests a new progressive stream with an explicit `startTimeTicks` while the old decoder is still shutting down, matching the C implementation's server-side seek method while shortening the handoff. When the replacement stream is ready, the old decoder stops and its Jellyfin stop/save reports finish asynchronously. The new offset overrides Jellyfin's saved resume position. The loading overlay covers startup, and subsequent progress includes the new offset. If the video was paused, the client pauses the replacement player when its first position arrives. A cancels a pending seek and stops playback.

## Loading and buffering

Media requests allow up to 60 seconds for Jellyfin to return response headers. Expensive transcodes, including HDR tone mapping during a seek, can exceed the former 15-second limit. Back and a new seek destination still cancel the pending request immediately.

Video output shows a centered animated loading indicator while Jellyfin prepares the stream and the player starts. The indicator clears when the player reports playback progress. Inline Ghostty video enables caching for the media pipe and reports libmpv's `paused-for-cache` property through the helper's optional `--status` protocol. A cache stall shows an animated buffering indicator over the last frame. Resuming playback restores the clean frame. User pause suppresses both indicators and keeps the existing clean pause behavior.

FFplay does not expose the same cache signal through the current adapter. Its buffering indicator is an estimate based on three seconds without advancing playback position. The indicator appears in Ghostty, while FFplay owns its separate video window. On MiSTer, the native output backend scales and publishes the same overlay through `/tmp/misterfin_crt_overlay`. The Go-specific MPlayer `vo_fbdev` driver validates the file, composites its cropped BGRA pixels after decoding each frame, and keeps a clean copy beneath the overlay for paused redraws.

## Live TV

Select a channel with B or Enter to tune it immediately. A stops the stream and returns to the channels list with the same channel selected. If playback ends or fails, the browser also returns to the channels list. Live TV works with both desktop player modes. On MiSTer and inline Ghostty, the View menu offers Original and Zoom without reopening the stream. Subtitles offers locally decoded closed captions when available. Live audio-track selection remains unavailable. Picture mode resets to Original when the channel is reopened.

The client posts the C device profile to `/Items/{id}/PlaybackInfo`, requests automatic tuner opening, and uses the returned transcode URL and session identifiers. The profile requests progressive MPEG-2/MP3 transport streams rather than direct tuner playback. The client removes the incompatible MPEG-2 level hints, matching the C workaround, and preserves the other negotiated parameters. Session reports include the media source and tuner identifiers with seeking disabled. Channels start live and never write movie resume or watched state.

Stopping, stream failure, player failure, and invalid negotiation responses release the returned tuner ID. If the user cancels while negotiation is running, Go lets the bounded request finish so it can read and close the acquired tuner ID. The harness gives Go up to 30 seconds to finish shutdown before forcing termination.

## Stream and session behavior

The stream query follows `jf_stream_url` in `src/jellyfin.c`: progressive MPEG-2 video in MPEG-TS, stereo MP3 at 48 kHz, no video stream copy, a 720×576 maximum frame, 12 Mbps video, and a 25 or 30 fps cap based on the display mode. Each attempt gets a unique play session ID. Custom transcode profiles remain pending.

Go owns HTTP and TLS. Video reaches the player through an anonymous pipe. Controllable music uses the private loopback adapter described above. Player arguments contain no Jellyfin URL or token. TLS verification follows the browser configuration, and redirects remain limited to the configured server origin. Player output is consumed only for numeric playback positions. Raw player diagnostics are not printed or logged.

The client reports session start after receiving player position feedback, then sends progress every ten seconds. It also persists the per-user resume position through the C client's user-data endpoint. Stopping cancels the stream, terminates the player process group, reaps the player, and sends a stopped report. A successful exit near the known end of the item marks it watched and clears its resume position. Canceling playback does not newly mark an item watched. Startup failure does not overwrite its resume position. Cleanup reports have a five-second deadline and are best effort if the server is unavailable.

After playback ends, the browser refreshes the details and reuses cached artwork when its image tags are unchanged. Metadata and playback failures return to the browser with an error message.

UI [navigation sounds](GO_SOUNDS.md) release the audio device before any media player starts and remain silent throughout music or video playback. MiSTer menu music is a separate service coordinated as described below.

## MiSTer menu music

The native browser coordinates with the optional MiSTer BGM service through `/tmp/bgm.sock`, matching the C client's integration. At startup, it reads the configured playback mode and stops an enabled `random` or `loop` playlist. Checking the mode also handles the gap between tracks, when the service can report that nothing is currently playing. A disabled playlist remains disabled.

After Jellyfin playback, input, and output cleanup finish, the browser sends `play` only if its earlier `stop` command was delivered. Restoration runs on normal exit, handled termination, and errors after suspension. It restarts BGM playback without restoring an exact track position. Missing services, invalid status replies, and socket errors leave the application usable. Each command has a 250 ms deadline. Ghostty and preview mode do not contact BGM.

The implementation lives in [`internal/mister/bgm`](../internal/mister/bgm/bgm.go). The MiSTer target supplies it through an optional application-lifetime hook, so BGM adds no work to rendering or playback loops. Tests use a private Unix socket and cover enabled/disabled modes, gaps between tracks, fragmented and malformed replies, missing services, timeouts, failed stop delivery, and restoration at most once. The maintainer's MiSTer had no standard BGM script or active socket during this implementation, so live add-on validation remains pending.

## MiSTer use and remaining work

The September 12 kernel build fixed framebuffer access. The user confirmed visible browser output and playback on MiSTer. Launch from the Scripts menu so Main_MiSTer enables CRT output. The installed launcher uses `/media/fat/misterfin-crt/mplayer-arm`, separate from the C player. Both Go binaries persist on the SD card. The C player cannot display Go playback overlays or accept the live picture command.

The hardware path uses `/media/fat/misterfin-crt/mplayer-arm`, separate from the C client’s player. Hardware video uses the C player’s `-framedrop` policy and `-autosync 30` for recorded media (`-autosync 1` for live channels). These settings let MPlayer correct video lag against the audio clock. The player does not force `-fps` or change playback speed. It currently supports 640-pixel-wide PAL and NTSC framebuffers, including doubled 480/576-line output. It uses the source display aspect ratio for letterboxing, ALSA audio, and the Go-specific framebuffer output driver. Recorded video and Live TV use the `misterfin` scaling filter for Original/Zoom changes within the running player. The output driver consumes the same Go-rendered loading, buffering, seeking, and playback-control overlays as the headless backend. Build it with `docker/Dockerfile.misterfin-crt`, which applies `docker/vo_fbdev_go.patch` to a private build copy without changing the preserved C player source. The direct Go binary accepts `-player` to override the executable path. In headless mode the executable must accept FFplay arguments. On hardware it must accept MPlayer arguments and include the Go overlay adapter.

```sh
docker build -f docker/Dockerfile.misterfin-crt -t misterfin-crt-mplayer docker
docker run --name misterfin-crt-mplayer-build misterfin-crt-mplayer
docker cp misterfin-crt-mplayer-build:/build/mplayer-arm build/misterfin-crt-mplayer-arm
docker rm misterfin-crt-mplayer-build
```

Update the Go client and Go-specific MPlayer together. The native output backend keeps the loading animation moving until MPlayer presents its first frame. Both processes coordinate that handoff through `/tmp/misterfin_crt_overlay.lock`. An older player does not claim the lock and must not be paired with the updated client.

MPlayer signals its first presented frame immediately so the loading label clears without waiting for the next position poll. Position reports still determine seeking and Jellyfin resume data.

The existing [Dockerfile](../docker/Dockerfile.misterfin-crt) builds the patched ARM MPlayer on the development machine. Docker is not required on MiSTer and does not build the Go client. Build the player against Bullseye’s glibc 2.31 toolchain, as specified by the Dockerfile. The tested MiSTer has glibc 2.31. An older saved artifact required glibc 2.35 and could not start. Rebuild the image from this Dockerfile before producing a replacement artifact.

Recorded-video subtitle and audio-track selection, Live TV closed captions, Original picture mode, and Zoom are implemented as described in [GO_TRACKS.md](GO_TRACKS.md). Optional true interlaced CRT output is described in [GO_DISPLAY.md](GO_DISPLAY.md). Zaparoo DDR integration is deferred. Additional hardware layouts remain unverified. Existing browsing, seeking, overlays, and music have been confirmed on the maintainer’s CRT. The maintainer also confirmed the new subtitle and audio-track selection works in testing. See GO_TRACKS.md for validation details.

## Rendering architecture

The concrete `PlaybackController` owns decoder handoff and media UX transitions. The shared video renderer consumes its value snapshot. See [the controller and rendering architecture](GO_RENDERING.md) for the boundaries and remaining coupling.

## Validation

Go tests cover the C stream query, unique session IDs, fragmented player feedback, invalid positions, CRT aspect calculations, normal completion, startup failure, cancellation, stopped reports, and watched/resume persistence. If FFmpeg and FFplay are installed, a test generates a three-second MPEG-2/MP3 clip and decodes it with FFplay using dummy SDL output. That test validates decoding and position feedback without opening a visible window.

Generated-media libmpv tests cover PAL and NTSC frame sizes, letterboxing, position feedback, completion, invalid media, and stopping before and during decoding. Output-boundary tests cover alpha composition, clean decoder-frame ownership, physical line doubling, cropped native overlay publication, and cleanup. Those tests use null audio and skip when libmpv or FFmpeg is unavailable. A generated four-second clip also completed through this workstation’s desktop audio service.

The browser integration test uses a controlled player process and mock HTTP server to exercise details → playback → stop → details → library navigation. Inline tests verify video pause/resume, menu reveal, clean-frame restoration after hiding or expiry, pause session reports, and return to browsing. Concurrent HTTP handling allows the media connection and API requests to proceed independently.

```sh
make test
make test-browse
go test -race ./...
go vet ./...
make arm
```

Host tests with and without cgo, race checks, `go vet`, Ghostty helper tests, nine browser integration tests, and the ARM cross-build passed. A five-second Live TV stream from the configured Jellyfin server decoded through the inline helper with muted audio and produced a 640×240 frame. The client then stopped and ran its cleanup. Mock-server tests verify the exact C negotiation profile, URL handling, cancellation during negotiation, tuner release on failure or stop, session identifiers, and absence of channel resume writes. Generated FLAC tests verify original-audio decoding with FFplay and libmpv, position feedback, and direct-stream reporting. Photo tests cover the C image query, decoded-size limits, PAL and NTSC aspect ratio, and returning to the parent folder. Browser tests cover clean music pause/resume, control reveal, track changes, automatic advancement, photo navigation, and restored folder selection. Additional tests cover byte-range forwarding, hidden-control expiry, cross-page navigation, and pause/resume through the complete Go/libmpv audio path. The preceding checks are the original milestone validation. Subsequent maintainer testing confirmed CRT playback and overlays. See GO_TRACKS.md for the newer track-selection validation boundary.

## Live TV aspect ratio on MiSTer

The native player uses video geometry from Live TV playback negotiation when available. When no source geometry exists, it uses the C client's 16:9 fallback instead of assuming 4:3. At 640×240, a 16:9 source occupies 640×180 with centered letterboxing. Explicit 4:3 metadata fills the 640×240 frame. Channels without metadata still rely on the fallback rather than automatic detection of aspect changes during a broadcast.

The Go MPlayer adapter decodes into a clean frame in RAM. Before each framebuffer row is written, the adapter blends the latest Go overlay into that row in RAM. Scanout never sees a bare video write followed by a separate menu paint. Overlay publication can run at 30 Hz independently of video presentation. Paused refreshes reuse the clean frame to prevent accumulated transparency and restore video when controls hide. `tools/test_native_overlay.py` tests the patched C adapter against memory-backed framebuffer pages.

Any direction shows or dismisses the menu during video and music playback, including while seeking. Photos retain Up to toggle controls and Left/Right to navigate. MiSTer reads controllers and keyboards directly through Linux input events, matching the C client’s button mapping. MiSTer’s synthetic action keys are ignored, so Xbox Y no longer acts as SELECT. Ghostty continues to use terminal keyboard input.

On MiSTer, the framebuffer adapter keeps the console in graphics mode for the app’s lifetime. The Go-specific MPlayer restores the console mode it inherited when playback ends, so loading and cancellation do not reveal startup console text. The app restores the original console mode when it closes.

## Music backgrounds and shuffle

Music supports whole-library shuffle from the artist list, stereo level meters, and configurable backgrounds. SELECT/Tab starts shuffle while browsing artists and cycles backgrounds during music playback. See [music playback and configuration](GO_MUSIC.md).

## ARM color conversion

The private MPlayer build patches its bundled FFmpeg ARM YUV-to-RGB wrapper to return the number of converted rows. The original wrapper returns zero even after writing the complete image. This affects videos whose decoded dimensions already match the output, such as 640×480 content in 480i mode. The picture filter correctly rejects an incomplete frame, so the incorrect return value previously left audio playing without video. The patch preserves the accelerated conversion and its pixels. A regression test checks the wrapper contract, and hardware probes verify all four supported frame heights.

For resized 480/576-line output, the picture filter scales in planar YUV before converting to RGB through the ARM NEON path. This avoids the slower scalar color conversion used by a combined resize-to-RGB operation. The intermediate buffer and conversion contexts are reused across frames. Progressive output and sources that already fit retain their existing conversion path.

On the maintainer’s MiSTer, a 720×404, 30 fps Live TV stream initially dropped seven frames in about 20 seconds. After separating resize and color conversion, scaling averaged about 10 ms per frame instead of 15 ms, with zero decoder drops over a 68-second sample. This sample does not establish frame pacing for every channel or source.
