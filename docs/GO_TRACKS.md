# Video options

Recorded movies, episodes, videos, and music videos support subtitles, audio-track selection, and picture modes. Press SELECT during playback to open Options. The default keyboard key is Tab. The Xbox default is View/Back. The playback controls show the configured button label for every recorded video. Live TV and music do not use this picker.

## Controls

- Left/Right moves between the Subtitles, Audio, and Picture tabs.
- Up/Down selects a row. Held directions scroll the picker.
- B/Enter applies the selected choice and dismisses the picker.
- A/Esc or SELECT/Tab closes the picker without stopping playback or showing the playback controls.
- Outside the picker, any direction retains its existing show/hide-controls behavior.

Subtitles includes an Off row. Audio includes Server default. Track descriptions use Jellyfin's display title, language, codec, and available title/forced information. The active choice has an asterisk. Long lists scroll within the CRT safe area. Input profiles configure the physical buttons and displayed names as described in [GO_INPUT.md](GO_INPUT.md).

## Remembered choices

Go remembers picture mode, audio track, and subtitle selection for each movie or episode. Stopping and resuming, restarting from the beginning, and restarting the application preserve those choices. Choosing Original, Server default, or Off replaces the previous choice. A video without saved choices starts with Original, server-default audio, and subtitles Off. Preferences are local to this Go installation and separate for each Jellyfin server and user.

Choices are saved under `playback` in the Go state directory alongside `session.json`. The existing `--state-dir` option selects that directory. Saved records contain the media source and selected stream metadata, with no credentials or subtitle text. Missing or changed tracks fall back to server-default audio or subtitles Off. Replacing a media source resets track choices while preserving picture mode. Text subtitles are downloaded again when playback reopens.

The MiSTer launcher uses `/media/fat/misterfin-crt/state`, so choices survive reboot. Ghostty uses the Go user configuration directory by default. A custom state directory must use persistent storage if choices must survive a reboot.

A background writer coalesces changes and replaces each record atomically. The in-memory copy supports immediate resume before a disk write completes. Only started playback and successful live changes update preferences, so canceled preparation and failed replacements leave previous choices intact. Shutdown flushes pending writes and reports failures. Subtitle timing adjustments remain limited to the current playback session.

## Picture modes

Picture offers Original and Zoom. Original is the default and preserves the entire picture with its correct aspect ratio. Zoom enlarges widescreen video and crops its sides. Scaling uses the encoded frame's display aspect ratio, so black bars within that frame can remain. Zoom does not detect the boundaries of the visible picture or guarantee that it fills the screen. Sources at or narrower than 4:3 keep their original fit.

On MiSTer and inline Ghostty, changing picture mode updates the running player without seeking, reopening the stream, or changing pause state. The Picture tab uses the same full-screen panel as Subtitles and Audio and closes after a successful change. A paused comparison redraws exactly the same decoded frame. Playing video continues normally. The active marker follows the player's acknowledgment. Failed live requests leave the preceding mode active and the picker open.

The choice survives seeking, audio changes, subtitle changes, and reopening the video. Separate-window FFplay still uses the existing stream handoff for picture changes. Applying a different mode dismisses the picker and reloads the stream at the current position.

`playback.PictureMode` describes the shared choice. MPlayer and the Python helper implement `pictureSetter`, which advertises `VideoTracks.LivePicture` to the browser. MPlayer receives `pausing_keep_force misterfin_picture <mode> <request>`. The Python helper receives `picture <mode> <request>`. Both reply with `ANS_PICTURE_MODE=<request>,<mode>`, where -1 reports failure. The browser matches replies to the decoder generation and request, with a handoff fallback for decoders without this capability.

The Go-specific `vf_misterfin` filter owns CRT scaling and a retained planar source frame. Original scales the full picture and adds black bars. Zoom crops the source's sides before the same single scaling pass. Two reusable scaler contexts avoid rebuilding scaling state on repeated toggles. Source rows are aligned for ARM. Cached input supports paused redraws without decoding another frame. The filter preserves source timestamps and keeps framebuffer geometry fixed, so mode changes do not reopen output or change the audio clock. Live TV keeps its existing scaling chain.

Inline libmpv changes its [panscan property](https://mpv.io/manual/stable/#options-panscan) on the existing square-pixel 4:3 render surface. The helper reads the decoded display aspect ratio and leaves sources at or narrower than 4:3 unchanged. Property changes trigger redraws through the existing render callback, including while paused. FFplay crops using the decoded sample aspect ratio. All players apply picture fitting before shared UI composition, so Go-rendered controls and subtitles retain their normal size. Server-burned subtitles remain part of the video and can be cropped.

Live Ghostty tests toggle repeatedly while paused, verify identical frames when returning to each mode, and verify that position stays fixed until resumed. Generated media covers widescreen, native 4:3, square, PAL, and NTSC cases. Browser tests verify that live picture changes do not reopen the media stream and that seeking and restarting the application preserve the selected mode.

Validation includes the Go suites with cgo enabled and disabled, race checks, `go vet`, host/ARM builds, 38 Python renderer/player tests, and 21 browser integration tests. Native filter tests cover geometry, padded input strides, 100 repeated toggles, stable timestamps, and no output reconfiguration. A playback-session test confirms native picture requests open only one media stream. On MiSTer, four live toggles while paused retained position 1.5 seconds. Captured Original frames matched byte for byte after toggling, as did repeated Zoom frames.

A generated 720×576 benchmark with null output measured about 4.21 seconds for 300 frames through the preceding Original filter chain and 4.82 seconds through the new chain. Retaining a source frame adds about 2 milliseconds per frame in that test. Zoom measured about 6.09 seconds. These measurements establish the scaling cost for that fixture. They do not establish CRT timing or playback performance for every source. The deployed test player must be checked with the maintainer's media.

## Text subtitles

On MiSTer and inline Ghostty video, Go downloads the selected subtitle as SubRip and renders it through the shared video overlay. Selecting text or Off does not restart the decoder unless an existing subtitle is burned into the stream. Downloads run asynchronously. A new selection cancels the preceding request, and late replies cannot override the latest choice. A failed download leaves the previous subtitle active.

Go removes supported SubRip markup and ASS override codes, preserves line breaks, and displays up to three lines with a dark outline. The existing font supports ASCII and Latin-1. Complex ASS styling, positioned signs, and additional writing systems are not reproduced.

While the Subtitles tab is open with client-rendered text selected, LT/RT or J/L adjusts timing in 0.1-second steps, within ±10 seconds. Earlier makes captions appear sooner. Later makes captions appear later. The displayed delay survives seeks and track changes for the current item and resets when another playback session starts. Image subtitles and FFplay burn-in do not have client-side timing controls.

Subtitle cues use absolute source times. Position feedback anchors a short interpolated clock between decoder reports. Pause freezes that clock. Seeking suppresses the old cue until the replacement reports its position. Downloaded text survives a seek or audio change, avoiding another extraction request.

## Audio changes and subtitle burn-in

Changing audio requests a new transcode at the current playback position. Image-based subtitles, including PGS and VobSub, use Jellyfin's `subtitleMethod=Encode`. Changing or disabling a burned-in subtitle also replaces the stream. Unknown subtitle codecs use this fallback.

The existing playback handoff prepares the replacement while retaining the preceding decoder, waits for its source to open, then transfers output ownership. Pause state and selected tracks survive the handoff and subsequent seeks. If preparation fails before the preceding decoder stops, playback resumes with its original selections.

A separate FFplay window cannot display Go overlay pixels over its video. That decoder therefore requests server burn-in for text subtitles too. The picker remains in Ghostty. MiSTer and inline Ghostty receive the same Go-rendered subtitle pixels without decoder-specific UI logic.

Starting an item from its details screen restores its saved selections after validating them against current source metadata. Jellyfin start/progress reports include the selected stream indexes and media source ID.

## Implementation

`jellyfin.MediaStream` and `MediaSource` describe server indexes and source identity. Playback details request `MediaSources` in addition to ordinary detail fields. Subtitle extraction uses the authenticated `/Videos/{item}/{source}/Subtitles/{index}/Stream.srt` endpoint. Stream replacement adds `audioStreamIndex`, `subtitleStreamIndex`, and, for burn-in, `subtitleMethod=Encode` to the existing transcode query.

`playback.VideoTracks` publishes immutable metadata through the decoder event bridge. `subtitleLoader` owns request cancellation and rejects obsolete results. `subtitles.Track` parses and indexes bounded cue data. `PlaybackController` owns picker navigation, selection intent, and subtitle timing. Its snapshot supplies `TrackMenu` and the current subtitle string to `RasterRenderer`. The existing output backends present the finished overlay. No framebuffer or MPlayer protocol change is required.

## Validation

Tests cover stream indexes that differ from row positions, source selection, text extraction authorization, markup and overlapping cues, cancellation, stale replies, Off during a pending download, failure recovery, pause restoration, selection persistence across seeks, and picker input routing. An integration test drives the Go binary through text selection, audio replacement, seeking, Off, and image-subtitle burn-in requests.

Persistence tests cover immediate reopen, application restart, restart from the beginning, account and item isolation, changed streams, damaged records, and unwritable storage. A browser integration test restores Zoom, alternate audio, and text subtitles after both stopping and restarting the app. Native protocol tests check that acknowledged picture changes update saved choices.

The maintainer's Jellyfin server returned Akira's alternate audio and PGS tracks. A real SubRip export from another movie parsed into 1,819 cues. Those server checks establish metadata and text extraction. The maintainer subsequently confirmed that subtitle and audio-track selection works in testing. That confirmation covers the tested setup and media, not every subtitle format or source.
