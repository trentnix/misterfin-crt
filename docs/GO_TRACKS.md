# Video subtitles and audio tracks

Recorded movies, episodes, videos, and music videos support subtitle and audio-track selection. Press SELECT during playback to open Tracks. The default keyboard key is Tab. The Xbox default is View/Back. The playback controls show the configured button label when the item has subtitles or multiple audio tracks. Live TV and music do not use this picker.

## Controls

- Left/Right selects the Subtitles or Audio tab.
- Up/Down selects a row. Held directions scroll the picker.
- B/Enter applies the selected track.
- A/Esc or SELECT/Tab closes the picker without stopping playback.
- Outside the picker, any direction retains its existing show/hide-controls behavior.

Subtitles includes an Off row. Audio includes Server default. Track descriptions use Jellyfin's display title, language, codec, and available title/forced information. The active choice has an asterisk. Long lists scroll within the CRT safe area. Input profiles configure the physical buttons and displayed names as described in [GO_INPUT.md](GO_INPUT.md).

## Text subtitles

On MiSTer and inline Ghostty video, Go downloads the selected subtitle as SubRip and renders it through the shared video overlay. Selecting text or Off does not restart the decoder unless an existing subtitle is burned into the stream. Downloads run asynchronously. A new selection cancels the preceding request, and late replies cannot override the latest choice. A failed download leaves the previous subtitle active.

Go removes supported SubRip markup and ASS override codes, preserves line breaks, and displays up to three lines with a dark outline. The existing font supports ASCII and Latin-1. Complex ASS styling, positioned signs, and additional writing systems are not reproduced.

While the Subtitles tab is open with client-rendered text selected, LT/RT or J/L adjusts timing in 0.1-second steps, within ±10 seconds. Earlier makes captions appear sooner. Later makes captions appear later. The displayed delay survives seeks and track changes for the current item and resets when another playback session starts. Image subtitles and FFplay burn-in do not have client-side timing controls.

Subtitle cues use absolute source times. Position feedback anchors a short interpolated clock between decoder reports. Pause freezes that clock. Seeking suppresses the old cue until the replacement reports its position. Downloaded text survives a seek or audio change, avoiding another extraction request.

## Audio changes and subtitle burn-in

Changing audio requests a new transcode at the current playback position. Image-based subtitles, including PGS and VobSub, use Jellyfin's `subtitleMethod=Encode`. Changing or disabling a burned-in subtitle also replaces the stream. Unknown subtitle codecs use this fallback.

The existing playback handoff prepares the replacement while retaining the preceding decoder, waits for its source to open, then transfers output ownership. Pause state and selected tracks survive the handoff and subsequent seeks. If preparation fails before the preceding decoder stops, playback resumes with its original selections.

A separate FFplay window cannot display Go overlay pixels over its video. That decoder therefore requests server burn-in for text subtitles too. The picker remains in Ghostty. MiSTer and inline Ghostty receive the same Go-rendered subtitle pixels without decoder-specific UI logic.

Selections apply to the current playback session. Starting an item from its details screen resets to server-default audio and subtitles Off. Jellyfin start/progress reports include the selected stream indexes and media source ID.

## Implementation

`jellyfin.MediaStream` and `MediaSource` describe server indexes and source identity. Playback details request `MediaSources` in addition to ordinary detail fields. Subtitle extraction uses the authenticated `/Videos/{item}/{source}/Subtitles/{index}/Stream.srt` endpoint. Stream replacement adds `audioStreamIndex`, `subtitleStreamIndex`, and, for burn-in, `subtitleMethod=Encode` to the existing transcode query.

`playback.VideoTracks` publishes immutable metadata through the decoder event bridge. `subtitleLoader` owns request cancellation and rejects obsolete results. `subtitles.Track` parses and indexes bounded cue data. `PlaybackController` owns picker navigation, selection intent, and subtitle timing. Its snapshot supplies `TrackMenu` and the current subtitle string to `RasterRenderer`. The existing output backends present the finished overlay. No framebuffer or MPlayer protocol change is required.

## Validation

Tests cover stream indexes that differ from row positions, source selection, text extraction authorization, markup and overlapping cues, cancellation, stale replies, Off during a pending download, failure recovery, pause restoration, selection persistence across seeks, and picker input routing. An integration test drives the Go binary through text selection, audio replacement, seeking, Off, and image-subtitle burn-in requests.

The maintainer's Jellyfin server returned Akira's alternate audio and PGS tracks. A real SubRip export from another movie parsed into 1,819 cues. Those server checks establish metadata and text extraction. The maintainer subsequently confirmed that subtitle and audio-track selection works in testing. That confirmation covers the tested setup and media, not every subtitle format or source.
