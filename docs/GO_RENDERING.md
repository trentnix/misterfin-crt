# Playback controller and rendering

`PlaybackController` is a concrete Go struct in `internal/browser/playback_controller.go`. It owns the current item, active and pending decoder resources, seek debounce, cancellation, pause restoration, and playback notices. `browser.Run` borrows an input event channel, owns session lifetime, and dispatches actions, timer ticks, worker results, and typed decoder events. `browserSession` owns navigation, request cancellation, selection loading, media navigation, and presentation. Its handlers coordinate those responsibilities with the controller.

`Model` owns the navigation stack, music queue presentation, and photo menu timer. `PlaybackController` exclusively owns its private `playbackState`. The model and controller share no mutable playback state.

The browser submits every screen through `videoout.Output.Present(Frame)`. It has no direct `platform.Display` dependency. The selected backend handles browsing, video composition, and loading fallback.

```mermaid
flowchart TD
    Run["browserSession.draw"] -->|"Copies scene values"| Scene["Scene"]
    Model["Model: navigation, music queue, photo controls"] --> Scene
    Selection["selectionState.current: images and library count"] --> Scene
    Controller["PlaybackController.Snapshot"] --> Scene
    Scene --> Raster["RasterRenderer.Render (implements Renderer)"]
    Animation["animationState"] -.->|"Motion and timing"| Raster
    Cache["sceneCache"] -.->|"Prepared artwork"| Raster
    Raster --> Frame["videoout.Frame"]
    Frame --> Output["videoout.Output.Present"]
    Output --> Ghostty["framefile.Backend"]
    Output --> MiSTer["native.Backend"]
    Ghostty --> GP["platform.Presenter: headless frame output"]
    MiSTer --> MP["platform.Presenter: framebuffer output"]
    MiSTer --> Overlay["Overlay publication to patched MPlayer"]
```

`cmd/misterfin-crt/target_mister.go` and `target_desktop.go` assemble input, player settings, and output for their respective environments. `browser.go` owns their lifetime, opens input, and cancels and joins the reader after the browser returns. `paths.go` resolves configuration, session, and cache paths into `browser.Config`. The browser receives those paths and semantic input events without selecting hardware. Playback receives explicit `DecoderConfig` values without using the display mode to choose a protocol. Existing command-line flags still select the same defaults.

`browserTarget.activate` optionally acquires environment resources before the browser opens input. `runBrowser` defers the returned cleanup until playback, input, preferences, and output have closed. The MiSTer target uses this hook for [`bgm.Suspend`](../internal/mister/bgm/bgm.go), which stops enabled menu music and restores it on exit. Desktop targets leave the hook unset. Target construction itself performs no environment changes.

`browserSession` depends on `Renderer` and `videoout.Output`. Its `draw` method constructs a `Scene`, asks the renderer for pixels, and presents them. It owns frame pacing and the request to refresh paused video when an overlay changes. It does not implement animation or call concrete drawing functions.

## UI sound ownership

The browser borrows `sound.Feedback` alongside the renderer and output. Input handling compares navigation state before and after an action, then emits a navigation or confirmation cue only for a visible change. Media controls remain silent. `playbackDriver` suspends feedback on its worker before calling `playback.Run` and releases it afterward, including failed launches. Counted suspensions cover overlapping seek replacements.

`cmd/misterfin-crt/browser.go` loads sound configuration and owns `sound.Player`. Target assembly supplies an audio-device factory. The Linux targets reuse the nonblocking ALSA adapter in `internal/sound/alsa`. Shared sound scheduling, queue limits, clip gain, and playback handoff live in `internal/sound`. Audio callbacks never enter the rendering pipeline. See [GO_SOUNDS.md](GO_SOUNDS.md) for configuration and device lifetime.

## Event loop ownership

`View` retains a contiguous window of up to three server pages for paginated lists. `Model.Prefetch` requests a neighboring page within 24 rows of an edge. `retainPage` merges responses and discards distant rows while preserving the absolute selection. `centerSelection` positions the highlight near the middle except at the library ends. Loading and errors preserve existing rows. Workers never mutate published item slices.

`animationState` eases the absolute list scroll position, so rebasing the page window does not create a jump. `screenPainter.list` borrows a vertical slice of the output canvas to clip moving rows. The header and footer remain fixed, and scrolling needs no intermediate image or full-frame copy.

Only the event loop mutates `browserSession`. Workers capture their request inputs and send `workerResult` values through one channel. Each outcome has a concrete result type containing only the fields its handler accepts. Producers stop mutating referenced payloads before delivery. The result type dispatches itself without a shared kind tag or unused payload fields. `connectionManager` owns configuration and session loading, authentication cancellation, and its generation counter. It publishes a complete authenticated client and account-scoped selection loader to the event loop. Listing requests, selection loading, and media navigation each have an independent cancellation scope. Selection loading and media navigation retain their own generation counters. Their handlers reject obsolete results before changing the model.

`homeState` owns the combined Continue Watching snapshot and an independent request generation. Its worker calls `Client.ContinueWatching`, then sends a `homeResult` to the browser loop. The loop updates retained home/list views by item or series identity and seeds the home cover sample. Home metadata never delays library requests. The shared renderer draws these entries through the existing carousel and list paths.

Controller profiles are resolved at the input boundary before the browser receives semantic actions. Each `control.Event` carries immutable binding labels for its physical device. The session passes the last active labels through `Scene.Controls` to the shared overlay renderer. MiSTer virtual arrow echoes retain the physical device labels. Terminal events supply keyboard labels. Button codes, device capabilities, and axis thresholds stay in `internal/input/evdev`. See [input configuration](GO_INPUT.md) for profile matching and defaults.

MiSTer’s `evdev.navigation` merges controller directions and virtual keyboard echoes into one press and repeat stream per direction. Held directions follow the C repeat timing: a 350 ms initial delay, six 110 ms intervals, then 45 ms intervals. Releasing a direction resets its acceleration. Late polls emit one repeat without catching up in a burst. Repeat actions retain their `-repeat` suffix so holding any direction cannot repeatedly toggle playback controls. Photo controls still toggle with Up. Trigger axes use their advertised ranges with separate press and release thresholds (25% and 15% by default). Seek actions repeat every 250 ms after a 350 ms delay. Terminal input requests explicit keyboard event types when supported.

Handlers return whether an event requires an immediate redraw. `Run` performs that redraw in one place. Timer ticks update playback and render at the interval supplied by `Output.FrameInterval(scene.Video)`. Outputs can implement `FrameNotifier` to request an immediate redraw when a video frame arrives. Shutdown cancels the session context before waiting for decoder completion, so callbacks cannot block after event dispatch stops.

## Renderer contract

`Renderer.Render(width, height, Scene)` accepts positive logical dimensions and returns `videoout.Frame`. A renderer owns its animation, caches, and frame storage. It must not perform network/device I/O or change navigation. Calls are serial. Returned pixels are borrowed until the next render call, so the caller must present or copy them first.

`Scene` copies the current view and scalar UI values without exposing `Model` or `playbackState`. It carries artwork, a separate library count, status text, time, a `PlaybackPresentation` for music and video, and photo menu visibility. Position, pause state, and playback control visibility remain in the playback snapshot. The current view borrows Jellyfin item slices and detail pointers for the synchronous render call. Renderers must not mutate or retain those navigation references. Artwork is immutable after publication and may be retained for caching.

`RasterRenderer` implements the shared pixel renderer. For each frame, `renderScene` creates a temporary `screenPainter` that borrows the canvas, scene, animation values, and cache. It selects one screen method. Browsing screens then share footer and notice drawing. The painter owns no persistent state or resources. `animationState` owns selection easing and marquee timing. `sceneCache` owns prepared artwork. Reusable UI and overlay canvases are invalidated together when geometry changes. A replacement raster renderer can implement `Renderer` and be injected at application assembly without changing browser navigation or output backends.

The output split remains downstream of shared UI drawing. Ghostty composites clean decoder frames and UI in its backend. MiSTer publishes UI overlays while MPlayer owns the display and presents browser/loading frames itself otherwise. The optional companion backend supports a separate player window.

## Presentation contract

`videoout.Frame` contains a full BGRX `UI` frame, a straight-alpha BGRA `Overlay`, and a `Video` flag. `Video` expresses UX intent and stays true while loading or seeking, even when no decoder owns the display. The backend borrows pixel slices for the duration of `Present`. `Output.Geometry()` supplies the layout dimensions.

`Output.FrameInterval(video)` supplies the presentation cadence. Browser screens use 60 Hz. The frame-file backend also keeps a 60 Hz animation timer, while native and companion backends retain 30 Hz overlay updates because their players present video independently.

The frame-file backend implements `FrameNotifier` and watches atomic decoder publications with inotify. A new frame wakes the shared event loop immediately instead of waiting for the animation timer. Notifications coalesce when rendering falls behind. The watcher closes with the output backend, and the timer remains available for loading and paused overlays.

For browsing, photos, and music, each backend presents `UI` through its internal `platform.Presenter`. During video playback, the Ghostty `framefile` backend reads clean frames from its decoder file and composites `Overlay`. If no valid frame exists, it uses black. The native backend publishes the overlay for patched MPlayer and continues presenting the overlay over black until MPlayer has its first video frame. The companion backend composites the overlay over `UI` while a separate player window displays video.

`Acquire` and `Release` bracket the external decoder's lifetime. Each backend coordinates the actual display handoff. On MiSTer, Go takes an advisory lock while presenting each loading frame. MPlayer takes the same lock just before presenting its first frame and retains it until output teardown or process exit. Go then only publishes overlays until `Release`. The lock prevents simultaneous framebuffer writes without adding waits to subsequent video frames.

The lock file remains in place across decoder lifetimes so both processes always use the same inode. The Go client and its patched MPlayer must both implement this handoff.

The native driver emits `ANS_VIDEO_STARTED=true` once after writing its first frame. `positionWriter` passes that signal through `Callbacks.VideoStarted` to the browser's `PlaybackVideoStarted` event. The controller clears loading immediately without waiting for MPlayer's one-second position poll or inventing a position. Decoders without this signal retain the existing position-based fallback. First-frame state resets for every replacement decoder, and stale decoder events cannot clear loading for its replacement.

`Clear` discards stale playback output before a new item and after playback ends. The application creates the backend once and closes it before closing the underlying display. Backends accept `platform.Presenter`, which exposes only geometry and presentation. `platform.Display` adds `Close` for the application that owns the device. A new presenter can therefore implement pixel delivery without pretending to own display resources. The browser does not select a display path per frame.

The Ghostty harness watches completed Go frame writes. It uploads changed frames on arrival, subject to the configured presentation cap. Upload time counts toward that cap, and expired slots are skipped after stalls. Decoder frames, Go composition, and terminal uploads no longer wait for independent polling ticks.

The terminal presenter uploads the next image before changing any visible placement. It then places the new image and removes the old one inside a single [synchronized-output update](https://ghostty.org/docs/help/synchronized-output), preventing intermediate screen states during the swap. The pixel payload remains opaque RGB and does not blend consecutive video frames.

## Source map

Each backend lives in its own subpackage and exports `New` and a concrete `Backend` type implementing `videoout.Output`. The shared `videoout` package has no dependency on its implementations. Application wiring selects the implementation.

- [`main.go`](../cmd/misterfin-crt/main.go): signal handling and display lifetime.
- [`browser.go`](../cmd/misterfin-crt/browser.go): input, preferences, and output lifetime around the shared browser.
- [`target.go`](../cmd/misterfin-crt/target.go): target selection and the assembled dependency set.
- [`target_mister.go`](../cmd/misterfin-crt/target_mister.go): evdev input, native output, and MPlayer defaults.
- [`target_desktop.go`](../cmd/misterfin-crt/target_desktop.go): terminal input, frame-file or companion output, and helper precedence.
- [`paths.go`](../cmd/misterfin-crt/paths.go): storage defaults and the cache-root override.
- [`options.go`](../cmd/misterfin-crt/options.go): command-line parsing and mode validation before resources open.
- [`preview.go`](../cmd/misterfin-crt/preview.go): test-frame display and its optional wait.
- [`run.go`](../internal/browser/run.go): session lifetime, event dispatch, and the single redraw decision.
- [`session.go`](../internal/browser/session.go): session state, construction, and cleanup.
- [`connection.go`](../internal/browser/connection.go): configuration and session loading, authentication, account-scoped dependency construction, and stale-attempt rejection.
- [`session_requests.go`](../internal/browser/session_requests.go): listing requests plus event-loop handling for connection and page results.
- [`session_selection.go`](../internal/browser/session_selection.go): selected metadata and images, loading, error handling, and stale-result rejection.
- [`session_media.go`](../internal/browser/session_media.go): playback completion and asynchronous neighbor requests for photos and music.
- [`model_media.go`](../internal/browser/model_media.go): parent snapshots, adjacent selection, return navigation, music queue presentation, and the independent photo menu timer.
- [`session_input.go`](../internal/browser/session_input.go): action routing and screen-specific controls.
- [`session_result.go`](../internal/browser/session_result.go): concrete worker result types and event-loop dispatch.
- [`session_render.go`](../internal/browser/session_render.go): scene assembly, frame pacing, and presentation.
- [`scene.go`](../internal/browser/scene.go): read-only rendering input assembled from navigation, artwork, and one playback snapshot.
- [`renderer.go`](../internal/browser/renderer.go): replaceable renderer interface.
- [`raster_renderer.go`](../internal/browser/raster_renderer.go): concrete renderer and frame-buffer ownership.
- [`animation.go`](../internal/browser/animation.go): per-renderer motion and title timing.
- [`render.go`](../internal/browser/render.go): screen dispatch and uncached rendering entry points.
- [`render_layout.go`](../internal/browser/render_layout.go): frame-local `screenPainter`, CRT safe areas, title marquee, clock, and browsing footer.
- [`render_status.go`](../internal/browser/render_status.go): connection and Quick Connect screens.
- [`render_photo.go`](../internal/browser/render_photo.go) and [`render_music.go`](../internal/browser/render_music.go): photo viewing and music playback screens.
- [`render_details.go`](../internal/browser/render_details.go): artwork and metadata on item details.
- [`render_carousel.go`](../internal/browser/render_carousel.go) and [`render_list.go`](../internal/browser/render_list.go): library carousel and paginated lists.
- [`render_video.go`](../internal/browser/render_video.go): video companion backdrop and playback overlays.
- [`render_metadata.go`](../internal/browser/render_metadata.go): item titles, subtitles, and count labels.
- [`scene_cache.go`](../internal/browser/scene_cache.go): prepared artwork cache.
- [`output.go`](../internal/videoout/output.go): `Frame` and the shared `Output` interface.
- [`frame_file.go`](../internal/videoout/framefile/frame_file.go): Ghostty frame-file backend and video composition.
- [`native.go`](../internal/videoout/native/native.go): MiSTer backend, framebuffer ownership, and overlay publication.
- [`handoff.go`](../internal/videoout/native/handoff.go): MiSTer first-frame handoff shared with the patched MPlayer output driver.
- [`companion.go`](../internal/videoout/companion/companion.go): companion UI backend for a separate player window.
- [`display_linux.go`](../internal/platform/display_linux.go): framebuffer and headless presentation through the C adapter.
- [`ghostty_harness.py`](../tools/ghostty/ghostty_harness.py): terminal presentation of headless frames.
- [`video_player.py`](../tools/ghostty/video_player.py): libmpv decoding and publication of clean video frames.
- [`playback_controller.go`](../internal/browser/playback_controller.go): controller ownership, item lifecycle, input actions, and player commands.
- [`playback_seek.go`](../internal/browser/playback_seek.go): seek phases, debounce, retargeting, replacement launch, handoff, and failure recovery.
- [`playback_event.go`](../internal/browser/playback_event.go): decoder event types and dispatch, position updates, and completion handling.
- [`playback_driver.go`](../internal/browser/playback_driver.go): external-player launch, callbacks, and the dedicated decoder event channel.
- [`playback_process.go`](../internal/browser/playback_process.go): decoder resources, cancellation, start gate, and completion wait.
- [`playback_state.go`](../internal/browser/playback_state.go): controller-owned UI state, menu visibility, destination accumulation, and loading or buffering labels.
- [`playback_presentation.go`](../internal/browser/playback_presentation.go): pointer-free rendering snapshot and its construction.

The controller methods span lifecycle, seeking, and event files because those responsibilities have distinct transitions. The smaller state, process, and presentation types each live with their methods.

A seek starts with a destination preview and a 0.5-second deadline. When the deadline expires, `seekPreparing` captures the user's pause preference, pauses the original decoder if needed, and prepares a replacement. A ready replacement waits behind a start gate until the original decoder ends. Further seek actions cancel the replacement and enter `seekRetargeting`, which shows the destination again and renews the deadline without overwriting the original pause preference. The controller opens the gate only after preparation succeeds and the original decoder has released its output. Position feedback then restores the user's pause preference.

## Controller contract

- `Start(item, offset, paused, now)` starts an item and resets its playback UI state. An explicit zero offset implements SELECT restart.
- `Key(action, now)` handles pause, control toggling, video seek, and stop. The browser handles music track selection separately.
- `Tick(now)` starts a pending seek after the half-second deadline. Retargeting cancels the obsolete replacement and restarts that deadline.
- `Handle(event, now)` applies decoder feedback. It returns true when the active item ends and the browser must handle navigation or track advancement. Seek handoffs and stale decoder events do not end the item.
- `Snapshot(now)` returns display information without mutable pointers, decoder handles, navigation state, or output-specific flags.
- `Refresh()` asks the active player to refresh its paused output. The output adapters remain responsible for pixel presentation.
- `StopForTrackChange()` stops the current decoder without treating the stop as a user exit from playback.
- `Close()` cancels and waits for the tracked active and pending decoders.

`PlaybackEvent` identifies the decoder and one event kind: prepared, position, paused, buffering, or ended. Each `playbackProcess` groups its identifier, cancellation function, completion channel, asynchronous cleanup signal, preparation gate, and readiness flag. A `playbackLaunch` function connects the controller to real decoding. Tests supply a deterministic function that records launch requests and cancellation.

Playback input uses separate `controls`, `seek-backward`, `seek-forward`, `track-previous`, and `track-next` actions. The browser maps directions to `controls` only during playback. Video seeks use the existing replacement-stream controller. Music sends relative ten-second commands through the optional `player.AudioSeeker` decoder interface. MPlayer and the Python audio helper implement that interface. Decoder progress remains the source of actual playback position, including after a paused audio seek.

The controller has no dependency on `platform`, `videoout`, terminal input, fonts, or canvas drawing. `playbackDriver` wires `AcquireVideo` and `ReleaseVideo` callbacks to the selected output adapter. It sends `PlaybackEvent` values through a dedicated channel directly to the browser loop. Decoder feedback no longer shares the browsing and artwork result queue or allocates an event pointer per update. Progress can be dropped when the decoder event queue is full. Lifecycle events wait for delivery unless the application is shutting down.

## Selection loading and artwork ownership

`selectionLoader` coordinates metadata and images for the selected item. Its constructor receives the mosaic and artwork disk caches as one account-scoped dependency set. The connection worker finishes this construction before publishing the authenticated connection, so selection workers never observe partially attached persistence. It refreshes non-photo detail metadata on every visit, including watched state after playback. Detail images use the refreshed metadata. If metadata fails, images can still load from the existing item.

Photos start immediately without a detail request. List images and carousel cover samples wait for the existing 120-millisecond selection debounce.

Library counts start independently of carousel samples and images. A slow count does not delay covers, and slow images do not delay counts. `libraryCache` retains at most 32 libraries, with separate one-minute deadlines for counts and sample metadata. Metadata expiry does not evict decoded images.

`artwork.MosaicCache` persists each library's decoded collage separately from the C cache. Selection workers restore saved images before querying current sample IDs and tags, then delegate missing images to `artwork.Loader`. Cached pixels populate the existing image cache, so unchanged tags avoid both downloads and decoding after restart. The browser loop performs no cache file I/O. Counts remain independent of collage persistence.

A complete `covers` artwork update replaces the sample as a unit before individual cover updates arrive. This also clears an obsolete collage when a refreshed library is empty. Only complete, uncanceled samples replace the disk file, and identical contents do not rewrite it. Retry defers disk invalidation to the selection worker. See [cache locations and limits](GO_BROWSING.md#persistent-collage-cache).

`artwork.Loader` handles only image requests, normalization, and retention. Its three-request limit is shared across selections. `Cached` provides memory-only lookup, and `Restore` primes saved mosaic covers without replacing newer images. The package-private `artworkCache` retains at most 16 MiB and 128 decoded images. The cache performs no network requests. Cached images remain immutable after publication, and completed images survive selection cancellation.

The browser retains image batching and result ordering in `selection_images.go`. Carousel image workers retain the sample order when publishing completed covers. Metadata freshness and cancellation belong to `selectionLoader`. The artwork package does not import the browser or renderer.

`selectionState` belongs to the browser loop. It tracks the current selection, cancellation, generation, presentation data, and error. `selectionUpdate` distinguishes detail metadata, library counts, and artwork. Its generation is checked before any result changes the model or screen. `Artwork` contains only images. `Scene.LibraryCount` carries the count separately. Retry invalidates the selected item's metadata and images through `selectionLoader.forget`.

- [`selection_loader.go`](../internal/browser/selection_loader.go): detail loading, image coordination, debounce, cached snapshots, and retry invalidation.
- [`selection_library.go`](../internal/browser/selection_library.go): independent library counts and carousel sample queries.
- [`selection_update.go`](../internal/browser/selection_update.go): selection presentation data and typed progressive results.
- [`mosaic_disk_cache.go`](../internal/artwork/mosaic_disk_cache.go): cache locations, server/user isolation, atomic file replacement, and disk limits.
- [`mosaic_cache_format.go`](../internal/artwork/mosaic_cache_format.go): versioned manifests, bounded RGBA records, and checksum validation.
- [`library_cache.go`](../internal/browser/library_cache.go): bounded library metadata retention and separate expiry deadlines.
- [`artwork.go`](../internal/browser/artwork.go): immutable image inputs for rendering.
- [`cache_files.go`](../internal/artwork/cache_files.go): shared file mechanics and disk-budget inventory.
- [`artwork_disk_cache.go`](../internal/artwork/artwork_disk_cache.go): ordinary artwork persistence and retry publication guards.
- [`artwork_loader.go`](../internal/artwork/artwork_loader.go): image loader resources, dimensions, and request limit.
- [`artwork_cache.go`](../internal/artwork/artwork_cache.go): tagged image retention, eviction, and invalidation.
- [`artwork_fetch.go`](../internal/artwork/artwork_fetch.go): cache lookup, request slots, and RGBA normalization.
- [`selection_images.go`](../internal/browser/selection_images.go): independent detail images and bounded workers for carousel samples.
- [`artwork_update.go`](../internal/browser/artwork_update.go): image results and their application to displayed artwork.

## Jellyfin client ownership

The application shares one `jellyfin.Client`. Endpoint methods use the same HTTP transport, configuration, and authenticated session. Authentication mutates the session and must finish before concurrent browsing, artwork, or playback requests begin.

- [`client.go`](../internal/jellyfin/client.go): client construction, authenticated requests, response limits, and status errors.
- [`authenticate.go`](../internal/jellyfin/authenticate.go): saved-session validation, API-key sign-in, and Quick Connect.
- [`library.go`](../internal/jellyfin/library.go): library lists, details, counts, and carousel cover queries.
- [`images.go`](../internal/jellyfin/images.go): tagged image requests, bounded decoding, and resizing.
- [`item.go`](../internal/jellyfin/item.go): item metadata, pages, stream geometry, and browsing locations.

The query fields, endpoints, redirect policy, and authentication fallback order remain the same. Image decoding is separate from request construction so pixel bounds do not depend on HTTP behavior.

## External player ownership

[`playback.Run`](../internal/playback/player.go) shows the complete decoder lifecycle. It selects a decoder from [reusable configuration](../internal/playback/config.go), locates the executable, [prepares playback](../internal/playback/prepare.go), opens the source, waits for the controller’s start gate, and starts monitoring the process.

Startup creates one [`playback.Config`](../internal/playback/config.go) containing decoder settings, output geometry and paths, and the shared preferences store. [`playbackDriver`](../internal/browser/playback_driver.go) creates a fresh [`playback.Request`](../internal/playback/request.go) for each item or replacement decoder. The request carries the item, track choices, start position, control channels, and [`Callbacks`](../internal/playback/callbacks.go). `Run` reads both values without modifying them. A nil track selection restores that item's saved preferences. A replacement supplies the controller's current choices explicitly.

[`trackPreparation`](../internal/playback/track_preparation.go) holds saved choices and decoder capabilities while metadata is being resolved. Preparation state stays private to one invocation rather than accumulating in the reusable configuration.

- [`mediaSource`](../internal/playback/media_source.go) owns the authenticated stream or local audio proxy.
- [`playerProcess`](../internal/playback/process.go) owns process lifetime, pipes, stream copying, and decoder feedback channels. It delegates pause, polling, and refresh to the selected decoder.
- [`playbackSession`](../internal/playback/session.go) owns decoder monitoring, startup timeout, pause state, and the latest playback position.
- [`progressReporter`](../internal/playback/progress_reporter.go) owns ordered Jellyfin reporting on a separate worker.
- [`positionWriter`](../internal/playback/position_writer.go) parses numeric progress, first-frame feedback, and buffering feedback without forwarding decoder diagnostics.

The `player.Decoder` interface separates executable protocols from process and session ownership. Each implementation validates its settings and supplies its executable, arguments, input transport, and pause, poll, and refresh behavior. Implementations hold immutable launch configuration. `player.Control` lends them stdin and process-group signaling without transferring ownership of the child process. Player packages do not import playback, browser, or output implementations.

- [`player.go`](../internal/player/player.go): decoder contract, source transports, and optional picture, audio-seek, and meter interfaces.
- [`picture.go`](../internal/player/picture.go): shared picture choices and display-aspect metadata interpretation.
- [`mplayer/decoder.go`](../internal/player/mplayer/decoder.go): native geometry validation, MPlayer slave commands, CRT fit, audio filters, and synchronization arguments.
- [`mplayer/audio_meter.go`](../internal/player/mplayer/audio_meter.go): export-file allocation, sampling, and cleanup.
- [`ffplay/decoder.go`](../internal/player/ffplay/decoder.go): FFplay arguments, crop geometry, and process-group pause/resume signals.
- [`pythonhelper/decoder.go`](../internal/player/pythonhelper/decoder.go): helper validation, line protocol, and clean-frame destination.
- [`playback/decoder.go`](../internal/playback/decoder.go): configuration-to-implementation selection and executable lookup.

The external player sources retain their existing build and launch locations: [`docker/vf_misterfin.c`](../docker/vf_misterfin.c) for native scaling and [`tools/ghostty/video_player.py`](../tools/ghostty/video_player.py) for libmpv rendering. Those files implement decoded-video fitting. The Go player adapters send commands. Output packages compose or publish the shared UI overlay afterward. Reusing a player does not require reusing a target's input or display backend.

`Config.VideoDecoder` and `Config.AudioDecoder` hold independently selected `DecoderConfig` values. Startup resolves executable overrides and helper precedence. Playback validates the selected protocol and converts it into one implementation before session preparation. The playback package has no `Headless` setting.

Audio feedback is optional. Decoders implement `player.LevelConfigurer` to enable their transport for one request. MPlayer creates an export file implementing [`player.Meter`](../internal/player/player.go). `Run` removes the file after decoder cleanup, including when preparation fails. The Python helper reports levels through its status pipe and allocates no export file.

The shared lifecycle retains source authentication, startup gating, cancellation, feedback, and Jellyfin reporting. MPlayer and the Python helper use a local range-capable proxy for audio. Other streams use file descriptor 3.

The Go-specific MPlayer build also applies `docker/mplayer_go.patch` to dropped-frame timing. A dropped frame never reaches the output filters, so their timestamp still belongs to the previous displayed frame. The patch advances the playback timeline once for each skipped frame instead of reading that stale timestamp and counting the interval again when the next image arrives. This prevents a brief rendering delay from trapping playback in repeated frame drops and audio drift. Frame dropping, audio-clock correction, and framebuffer presentation timing retain their existing policies. The inherited C build remains unchanged.

Polling remains once per second. MPlayer sends a position query. FFplay and the Python helper already publish progress, so their poll methods do nothing. Paused-frame refresh sends a command only through MPlayer. The player interface introduces no frame copying or rendering work. UX construction and the downstream `videoout.Output` boundary remain separate from decoding.

The playback loop reaps the decoder before returning. Cleanup then cancels media work, stops stream copying, releases video output, closes the source, and finalizes reporting. Live TV closes its negotiated stream afterward. Stop and seek handoffs allow final reporting and tuner release to continue asynchronously. The browser returns to navigation after local decoder cleanup, without waiting for server requests. Reporting uses a copy of the completed session state. Normal completion retains synchronous cleanup.

`Callbacks.CleanupDone` sends a generation-tagged event after reporting and tuner release finish. When the stopped session is still current, the browser refreshes Continue Watching and the visible item's details. Older cleanup cannot alter a newer playback session. Reopening the same video before its resume save completes uses the last locally reported position. An explicit restart still takes precedence. The playback driver tracks detached cleanup, and application shutdown waits for those bounded requests to finish rather than discarding an outstanding resume save.

## Progress reporting ownership

`progressReporter` performs Jellyfin reporting outside the decoder monitoring loop. The loop queues copied state after the first position, every ten seconds, and after a successful pause/resume command. Controls, position feedback, buffering feedback, and decoder completion do not wait for those HTTP requests. Playback callbacks run on the playback loop. `CleanupDone` runs on the finalization worker once a session exists and sends completion through the browser's event channel. `Run` waits for that worker unless asynchronous cleanup is enabled. Preparation failures invoke the callback before returning.

The worker retains an initial start/progress job and at most one pending progress job. New progress replaces stale pending progress, including superseded pause state. Coalescing preserves a pending request to save resume data and uses the newest position and watched state. Start and stop are not coalesced. A short mutex protects the mailbox, and a buffered notification wakes the single worker. Network requests never hold the mailbox lock.

Finalization replaces pending progress with the final state. Normal completion finishes the initial or in-flight report before stop/save. Stop, decoder failure, and asynchronous seek handoffs cancel routine reporting. The final stop/save job has a fresh five-second context, so canceled reporting cannot consume its deadline. Routine reporting retains the shared Jellyfin client's request timeout. The single worker sends requests in order and exits after final cleanup.

Routine reporting failures remain visible when decoding completes successfully. Intentional cancellation does not become a reporting error. Final stop/save failures remain best effort, preserving the existing error policy. State snapshots also copy optional boolean fields so asynchronous cleanup cannot observe later caller mutations. The reporting worker adds no work to frame rendering or stream copying.

## Navigation and playback ownership

`PlaybackPresentation` carries decoder activity, media type, title, position, duration, seek destination, pause state, control visibility, wait label, and notice. `renderVideoOverlay` accepts the snapshot and time for animation. It no longer inspects the browser view stack or Jellyfin item.

`PlaybackController` owns `playbackState` as a private value. Browser handlers send actions and decoder events to the controller. Rendering receives only its value snapshot. `sceneFromModel` combines that snapshot with navigation and artwork once per frame. `draw` uses the completed scene for frame pacing, rendering, and paused-overlay refresh.

`Model` keeps the music queue screen active while the decoder stops and the next track is found. Decoder completion alone does not return to the list. `SelectAdjacent` commits the parent page, selection, detail item, and title together after a matching result arrives and the old decoder stops. `ReturnToParent` restores the saved list position, invalidates listing results, and clears queue and photo menu state. Worker cancellation and generation checks remain in the session handlers.

Photo navigation uses its own three-second menu timer in `Model`. `Scene.PhotoControlsVisible` carries that visibility to the photo renderer. Playback menus and seek pins cannot change photo menu visibility. Adjacent photos retain an open menu until its deadline. Leaving and reopening a photo clears the menu.

Playback notices retain their existing behavior. Notices are available in the snapshot, but the video overlay does not yet draw them. Shared layout geometry, pixel formats, headless cgo usage, and frame transport are unchanged. The output interface now carries both browser and playback frames.

## Validation

`TestRenderScreenPixels` compares 40 PAL/NTSC screen cases against hashes captured before extracting the screen drawing methods. The cases cover connection screens, browsing modes, details, photos, music, and video overlays. Existing tests also compare cached and uncached output and verify overlay clearing. The 40 pixel baselines, overlay-clearing test, and video-backdrop cache test passed on both the host and MiSTer’s ARM CPU after the extraction.

Navigation tests cover adjacent selection, restoring the parent page and scroll position, stale listing rejection, and independent photo menu timers. Session tests cover the music screen between decoder runs, committing a queued track only after decoder exit, queue completion, stop, and canceled neighbor results.

Controller tests use explicit times and simulated decoder events. They cover the half-second debounce, destination-to-seeking transitions, retargeting during old-decoder shutdown, immutable request offsets and snapshots, stale feedback, pause restoration, seek failure, Back cancellation, control expiry, and Live TV seek exclusion. Ghostty integration tests continue to exercise the real event loop and player bridge, including restart followed by a slow seek, music track changes, photos, and returning to the channel list.

Reporting tests hold a server request open while exercising pause, resume, buffering feedback, position updates, and cancellation. They also cover bounded progress coalescing, save intent, request ordering, snapshot ownership, asynchronous cleanup, reporting errors, and immediate decoder completion.

Decoder tests cover implementation selection, executable overrides, audio-helper precedence, invalid modes, source transport, exact control commands, process signals, and control transport errors. A comparison against the pre-refactor command builders matched 1,440 argument variants across media types, output sizes, aspect ratios, source forms, and player choices. Existing lifecycle tests cover real decoding, pause/resume, startup gates, cancellation, seek cleanup, live-stream release, and native synchronization arguments.

Output tests cover normal browser frames, loading before and after decoder launch, the first-frame handoff, loading after release, return to browsing, stale video files, composition, and native overlay publication. The C adapter tests verify that decoding into RAM leaves the loading lock available and that presenting the first frame claims it until output teardown.

## MiSTer drawing performance

On September 12, 2026, `BenchmarkBackdrop` measured scaling a 1280×720 RGBA backdrop to 640×240 and shading it on the MiSTer ARM CPU. The original path took approximately 141 ms per operation and allocated 614403 bytes across 153600 allocations. Direct RGBA pixel access plus a shading lookup table reduced that to approximately 19 ms and 2688 bytes in one allocation. This benchmark measures drawing work, not complete menu latency or framebuffer presentation. Pixel comparisons cover transparency, clipping, subimages, and shading.

Run the benchmark with `go test ./internal/ui -run '^$' -bench BenchmarkBackdrop -benchmem`. For hardware measurement, cross-compile that package with `go test -c` and run the test binary on MiSTer.

## Visual parity review

The September 12 comparison used `src/main.c` and `src/draw.c` from the preserved merged C baseline. List backdrops now occupy the top three quarters of the logical screen, preserving their physical 16:9 shape, with the baseline's 110/255 brightness fading to black. Header marquees run at 15 pixels per second and restart when the screen title changes. The header uses a 16-row scratch canvas instead of allocating another full frame.

Runtime labels use hours once duration reaches one hour. Artist, album, and series rows omit missing counts and use singular labels for one item. Album rows omit missing years. Detail ratings follow the year when present and start at the left margin otherwise. Both channel type names use the same channel-number formatting. Regression tests cover PAL/NTSC backdrop bounds, metadata, duration formatting, and marquee clipping.

Remaining visual gaps include music visualizers and VU meters, animated setup screens, Continue Watching and Next Up cards, About/update screens, and advanced playback menus. Some require data or playback features as well as drawing. The maintainer's requested photo/music navigation, clean pause behavior, and shared loading/seeking overlay remain intentional Go UX requirements.

## Prepared artwork and frame pacing

`RasterRenderer` owns reusable browser and overlay frames, animation state, and `sceneCache` on the event loop. List backdrops and cover panels are composed once per image/geometry change. Detail backdrops and gradients are also cached. Carousel artwork is resized and shaded into row strips once, then scrolling copies a different window from each strip. Text, selection, clocks, and controls remain dynamic.

The cache retains one prepared background and one carousel set. It compares immutable RGBA image identities, screen geometry, and tile aspect. It rebuilds when those inputs change. Unknown image implementations bypass reuse. Returned frame pixels are borrowed until the next draw, matching the synchronous output contract. Standalone `render` calls keep the uncached path for independent captures and pixel comparisons.

Complete-frame `BenchmarkBrowserFrame` measurements on the MiSTer ARM CPU were approximately 19.2 ms for a list and 36.9 ms for a carousel before caching. After caching, both measured approximately 3.0 ms. Per-frame allocations dropped from approximately 659–666 KB to 43 KB. These benchmarks exercise drawing with prepared local artwork, including animated positions. They exclude network loading, framebuffer vsync/copy, and physical input latency. First draws after cache invalidation still prepare artwork.

Browser animations now request updates at 60 Hz, matching the C carousel timeline. Native video overlays retain 30 Hz polling. The native MPlayer adapter retains clean decoded pixels in RAM and blends the latest overlay before writing each framebuffer row. The overlay remains present on every video frame, even when the UI has not changed. The native player retains the C adapter’s vsync wait in the decode path, before MPlayer schedules presentation. Video backdrops are cached instead of rescaled at the overlay update rate.

Hardware framebuffer presentation still waits for vsync, so the physical display determines the achieved cadence. Pixel comparisons pass on MiSTer for cached versus fresh frames across animation, image replacement, transparency, subimages, and PAL/NTSC geometry changes.

Selection tests cover progressive details and covers, slow counts, independent metadata expiry, warm-cache reuse, bounded metadata retention, retry invalidation, canceled debounce, and stale results. Image tests cover the shared request limit across concurrent loads, image-only requests, RGBA normalization, and retained images after cancellation.

Renderer contract tests cover copied scalar state, complete browser/video frames, overlay clearing, geometry changes, and animation timing. Output tests use a presenter with no `Close` method to verify the narrower backend dependency.

After renderer injection, the benchmark calls `Renderer.Render`, including scene-driven animation and frame assembly. MiSTer measured approximately 3.2 ms per list/carousel frame, with two allocations per frame. Hardware renderer contract tests passed. The small difference from the earlier 3.0 ms draw-only measurement does not establish a change in input-to-display latency.

Video playback must have access to both MiSTer CPU cores. Main_MiSTer pins Scripts to CPU 1, so the Go launcher widens affinity before starting the application and its decoder. A captured 29.97 fps episode sample reproduced about 2.3 seconds of A/V drift under competing work on CPU 1. The same test with both cores available stayed synchronized with no dropped frames. Decoder startup must inherit the wider mask. Changing affinity after drift accumulated did not recover the failing session. Backdrop identity checks include JPEG `*image.YCbCr` and PNG `*image.NRGBA` images as well as `*image.RGBA`, so playback does not repeat image scaling at every UI tick.

## Music effects

`Scene` carries an immutable `musicviz.Library`, the selected background index, and a copied stereo audio measurement. `RasterRenderer` owns `musicviz.Renderer`, which advances the selected `musicviz.Effect` and draws into the same canvas as the music screen. The screen painter retains ownership of layout, progress, meters, and controls. File loading runs in a browser worker. Decoder measurements arrive as generation-tagged events. Effects never read files, control playback, or choose an output backend. See [music configuration](GO_MUSIC.md).

## Ordinary artwork persistence

`artwork.Loader` checks its bounded decoded-memory cache, then `artwork.DiskCache`, before requesting an image from Jellyfin. `artwork.DiskCache` stores covers, backdrops, and logos under the shared configurable cache root. `cache_paths.go` isolates accounts for both artwork and collages. The private `cacheFiles` implementation shares bounded reads, staging, atomic rename, and budget inventory. Each cache serializes its file operations and retains its own freshness rules.

Both caches scan existing files once, then track publications and removals in memory. Cache hits do not rewrite timestamps. Concurrent application processes sharing a directory do not coordinate their inventories.

`artwork_cache_format.go` preserves decoded RGBA pixels with a version and checksum, including logo transparency. Image tags and parent backdrop ownership determine identity. Photos keep their existing memory-only path because their requested dimensions depend on output geometry.

Image workers own disk I/O and pruning. Immediate selection snapshots remain memory-only. Retry invalidates a revision without waiting for file reads or pixel writes. Workers remove invalidated files and check their revision before publishing replacements. A canceled or superseded request cannot repopulate the memory cache or replace a newer disk entry. Artwork persistence does not add filesystem work to rendering or playback.

## Subtitle and track selection rendering

`PlaybackController` owns the video Options picker, picture-mode selection, and subtitle timing. Playback workers publish immutable stream metadata and parsed `subtitles.Track` values through `PlaybackEvent`. `PlaybackPresentation` supplies an immutable `TrackMenu` snapshot and the active subtitle text. `renderVideoOverlayOn` draws those values on the same overlay canvas as the existing playback controls. Native and inline outputs compose the resulting pixels. A separate-window decoder advertises that client-rendered subtitles cannot reach its video, so playback requests server burn-in for that decoder. The UI does not select a Ghostty-specific rendering path. See [GO_TRACKS.md](GO_TRACKS.md).

Picture mode affects decoded video before overlay composition. The shared `playback.PictureMode` passes through playback handoffs to the selected decoder. MPlayer and the Python helper advertise `VideoTracks.LivePicture` through the optional `player.PictureSetter` interface. Both accept picture controls through the running player and acknowledge them through the same event path. A successful acknowledgment updates the active choice and dismisses the Picture tab without showing playback controls. Paused changes reuse the same frame and timestamp.

MPlayer's `vf_misterfin` filter fits or takes a centered horizontal, vertical, or four-edge crop from the retained source frame, then scales once to the fixed CRT canvas. Inline libmpv changes video zoom on the existing render surface and publishes its redraw through the normal frame callback. FFplay retains the stream-handoff fallback. `RasterRenderer` uses the same full-screen panel for Subtitles, Audio, and Picture. Controls and text subtitles retain their normal size. See [picture modes](GO_TRACKS.md#picture-modes).
