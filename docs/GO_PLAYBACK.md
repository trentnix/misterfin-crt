# Go media playback

The Go client can start and resume movies, episodes, videos, and music videos through Jellyfin's progressive MPEG-2/MP3 transcode endpoint. Desktop playback opens FFplay in a separate window by default, with optional video inside Ghostty through libmpv. The MiSTer path launches the existing `mplayer-arm` executable and suspends browser framebuffer writes until the player stops. Live TV channels use the C client’s negotiated stream setup. The C application remains unchanged.

## Desktop use

FFplay must be installed and available as `ffplay` on `PATH`. Run the browser against a real Jellyfin server:

```sh
python3 tools/ghostty/ghostty_harness.py --browse --ntsc --config jellyfin.conf
```

Use `--pal` for PAL. Open a movie's details, then press B or Enter to play. If Jellyfin has an unwatched resume position, playback starts there. During playback, B or Enter in Ghostty pauses or resumes without adding instructions. Up reveals the controls in Ghostty for three seconds. A stops playback and returns to details. Any keypress in the FFplay window closes the video window. Q or Ctrl+C in Ghostty stops playback and exits the browser.

With the default FFplay mode, video appears in its own window and Ghostty shows the title and artwork. Up reveals the controls in Ghostty. Keep keyboard focus in Ghostty when using those controls. The inherited mock-server demo supplies browsing data and artwork but does not serve playable videos. Automated decoding tests use a generated video fixture instead.

## Video inside Ghostty

The host must have Python 3 and a shared libmpv library. This workstation already has `libmpv.so.2` version 0.37. Check that the helper can load the API, then launch the browser:

```sh
python3 tools/ghostty/video_player.py --check
python3 tools/ghostty/ghostty_harness.py --browse --ntsc --inline-video --config jellyfin.conf
```

Open a video's details and press B or Enter. Video replaces the browser image inside Ghostty. Audio plays through the desktop audio output. B or Enter pauses or resumes without a pause overlay. Up reveals the title, playback time, and controls over the video for three seconds. Pausing or resuming hides the menu immediately. A stops playback and restores the details screen. Q exits. The inline mode defaults to 30 terminal presentations per second. Use `--fps 25` to select a lower limit. The mock demo does not supply playable media.

The Python helper uses libmpv's [software rendering API](https://github.com/mpv-player/mpv/blob/v0.37.0/libmpv/render.h). libmpv handles decoding, audio, and presentation timing. A dedicated render thread writes complete BGRX frames atomically to a separate decoder frame file, using the output path with `.video` appended. Rendering at 640×480 before sampling PAL or NTSC rows preserves the harness's physical 4:3 aspect ratio and source letterboxing. The Go browser reads each clean decoder frame and presents it with controls only when requested. The menu never modifies the decoder frame, so hiding or expiring it restores the clean picture even while paused. Terminal presentation can drop frames if uploads cannot keep up. Visible smoothness and audible synchronization still need interactive Ghostty validation.

The Go process retains stream ownership and session reporting. The helper receives media on descriptor 3, reads pause commands from a separate standard-input pipe, and returns numeric playback positions. The direct Go binary accepts `-terminal-player tools/ghostty/video_player.py` with `-browse`, a 640×240 or 640×288 `-headless` geometry, and `-output`. The terminal helper cannot be combined with `-player`. MiSTer does not need Python or libmpv.

## Photos and music

Photos open full screen with no title or instructions over the image. Left and Right move between photos in the current folder, skip other item types, and fetch additional pages as needed. Up reveals the title, folder position, and navigation controls for three seconds. A returns to the folder with the current photo selected. R retries a failed image request. Photos remain manual, matching the C viewer. Slideshows and zoom remain pending.

The photo request uses Jellyfin's primary image at the logical framebuffer dimensions with quality 90, matching the C endpoint. The viewer preserves the source aspect ratio on the physical 4:3 display, including portrait letterboxing. Loading is cancellable and shares the bounded artwork cache.

Browse Music → artist → album, then select a track with B or Enter to start playback. Tracks advance automatically in list order, including across pages. The queue stops at the end of the album or at a non-audio item. A stops playback and returns to the track list with the current track selected. Q exits.

B or Enter pauses or resumes music without adding a pause overlay or instructions. Up reveals the controls for three seconds. Left and Right immediately select the previous or next track, whether the controls are visible or hidden. Up only reveals or refreshes the menu. Music controls do not seek within a track. B or Enter hides the controls immediately. The clean-pause and timeout behavior follows commit `bb31e83` and [the C pause UI](../src/pause_ui.c). That commit changed video controls. The Go music controls apply the same clean-pause behavior, and Up reveals photo navigation as requested.

Music uses the C client's `/Audio/{id}/stream?static=true` request, with a unique play session ID. It streams the original audio and reports `DirectStream`, including pause state and playback positions. Each newly selected track starts at the beginning. Session progress and completion use the existing reporting and user-data endpoints.

The Ghostty harness uses libmpv for controllable audio in both video modes. The decoder reads from a private loopback HTTP address. Go forwards byte-range requests to the fixed Jellyfin audio URL, which keeps the original stream seekable without exposing server credentials to the player. The adapter closes when the track ends or playback is canceled. The direct Go binary accepts `-audio-player tools/ghostty/video_player.py` for the same controls. Without an audio helper, direct headless use retains the FFplay fallback with pause control. On MiSTer, MPlayer uses the same audio adapter, native slave commands, and the C volume reduction and 48 kHz resampling filter.

Shuffle, repeat modes, and visualizers remain pending. Photo and music controls are covered by desktop tests. Physical CRT playback remains unverified.

## Live TV

Select a channel with B or Enter to tune it immediately. A stops the stream and returns to the channels list with the same channel selected. If playback ends or fails, the browser also returns to the channels list. Live TV works with both desktop player modes.

The client posts the C device profile to `/Items/{id}/PlaybackInfo`, requests automatic tuner opening, and uses the returned transcode URL and session identifiers. The profile requests progressive MPEG-2/MP3 transport streams rather than direct tuner playback. The client removes the incompatible MPEG-2 level hints, matching the C workaround, and preserves the other negotiated parameters. Session reports include the media source and tuner identifiers with seeking disabled. Channels start live and never write movie resume or watched state.

Stopping, stream failure, player failure, and invalid negotiation responses release the returned tuner ID. If the user cancels while negotiation is running, Go lets the bounded request finish so it can read and close the acquired tuner ID. The harness gives Go up to 30 seconds to finish shutdown before forcing termination.

## Stream and session behavior

The stream query follows `jf_stream_url` in `src/jellyfin.c`: progressive MPEG-2 video in MPEG-TS, stereo MP3 at 48 kHz, no video stream copy, a 720×576 maximum frame, 12 Mbps video, and a 25 or 30 fps cap based on the display mode. Each attempt gets a unique play session ID. Custom transcode profiles remain pending.

Go owns HTTP and TLS. Video reaches the player through an anonymous pipe. Controllable music uses the private loopback adapter described above. Player arguments contain no Jellyfin URL or token. TLS verification follows the browser configuration, and redirects remain limited to the configured server origin. Player output is consumed only for numeric playback positions. Raw player diagnostics are not printed or logged.

The client reports session start after receiving player position feedback, then sends progress every ten seconds. It also persists the per-user resume position through the C client's user-data endpoint. Stopping cancels the stream, terminates the player process group, reaps the player, and sends a stopped report. A successful exit near the known end of the item marks it watched and clears its resume position. Canceling playback does not newly mark an item watched. Startup failure does not overwrite its resume position. Cleanup reports have a five-second deadline and are best effort if the server is unavailable.

After playback ends, the browser refreshes the details and reuses cached artwork when its image tags are unchanged. Metadata and playback failures return to the browser with an error message.

## MiSTer use and remaining work

MiSTer playback remains blocked by the framebuffer failure. On September 9, 2026, the new ARM build ran its headless test on `192.168.1.42`, but opening the hardware framebuffer returned `open framebuffer: no such device`. The device still runs `6.18.38-MiSTer` and has the existing player. No installed executable was replaced.

The hardware path uses `/media/fat/misterfin/mplayer-arm`. It currently supports 640-pixel-wide PAL and NTSC framebuffers, including doubled 480/576-line output. It uses the source display aspect ratio for letterboxing, ALSA audio, and the existing framebuffer output driver. The direct Go binary accepts `-player` to override the executable path. In headless mode the executable must accept FFplay arguments. On hardware it must accept MPlayer arguments.

Video seeking, restart selection, subtitles, audio-track selection, shuffle, DDR output, HDMI layouts, and hardware video overlays remain pending. The MPlayer path accepts pause/resume commands, but its physical framebuffer overlay and timing still need hardware work. The first implementation deliberately covers starting a library video or live channel, reporting its session, stopping, and returning to browsing.

## Validation

Go tests cover the C stream query, unique session IDs, fragmented player feedback, invalid positions, CRT aspect calculations, normal completion, startup failure, cancellation, stopped reports, and watched/resume persistence. If FFmpeg and FFplay are installed, a test generates a three-second MPEG-2/MP3 clip and decodes it with FFplay using dummy SDL output. That test validates decoding and position feedback without opening a visible window.

Generated-media libmpv tests cover PAL and NTSC frame sizes, letterboxing, position feedback, completion, invalid media, and stopping before and during decoding. Those tests use null audio and skip when libmpv or FFmpeg is unavailable. A generated four-second clip also completed through this workstation’s desktop audio service.

The browser integration test uses a controlled player process and mock HTTP server to exercise details → playback → stop → details → library navigation. Inline tests verify video pause/resume, menu reveal, clean-frame restoration after hiding or expiry, pause session reports, and return to browsing. Concurrent HTTP handling allows the media connection and API requests to proceed independently.

```sh
make -f Makefile.port test
make -f Makefile.port test-browse
go test -race ./...
go vet ./...
make -f Makefile.port arm
```

Host tests with and without cgo, race checks, `go vet`, Ghostty helper tests, nine browser integration tests, and the ARM cross-build passed. A five-second Live TV stream from the configured Jellyfin server decoded through the inline helper with muted audio and produced a 640×240 frame. The client then stopped and ran its cleanup. Mock-server tests verify the exact C negotiation profile, URL handling, cancellation during negotiation, tuner release on failure or stop, session identifiers, and absence of channel resume writes. Generated FLAC tests verify original-audio decoding with FFplay and libmpv, position feedback, and direct-stream reporting. Photo tests cover the C image query, decoded-size limits, PAL and NTSC aspect ratio, and returning to the parent folder. Browser tests cover clean music pause/resume, control reveal, track changes, automatic advancement, photo navigation, and restored folder selection. Additional tests cover byte-range forwarding, hidden-control expiry, cross-page navigation, and pause/resume through the complete Go/libmpv audio path. Physical CRT playback remains unverified.
