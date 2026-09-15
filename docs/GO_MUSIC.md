# Go music playback

Music supports album queues and whole-library shuffle, previous/next tracks, pause/resume, ten-second seeking, stereo level meters, and selectable backgrounds. The music screen uses the same renderer on MiSTer and in Ghostty.

## Controls

On a music library's artist list, press the configured SELECT action to start whole-library shuffle. The keyboard default is Tab. The footer shows the active input's label. Shuffle requests up to 64 random audio tracks with the C client's `ParentId`, `Recursive=true`, `IncludeItemTypes=Audio`, and `SortBy=Random` query. It draws another batch when the queue runs out. Tracks can repeat between batches, but a batch boundary avoids immediately repeating the same track when another track is available.

During playback, shoulders or brackets select the previous/next track. Triggers or J/L seek ten seconds. Any direction toggles controls. B/Enter pauses or resumes without showing instructions. A/Esc stops and returns to the artist list or album track list. Shuffle preserves the artist selection and keeps at most two batches of history. Stopping also cancels a pending batch request.

During music playback, SELECT/Tab cycles backgrounds. The screen briefly shows the selected background's name. The selected background persists across tracks for the current application session. Restarting uses the configured default. [Input profiles](GO_INPUT.md) can change the controller binding and displayed label for SELECT.

## Backgrounds

| Name | Behavior |
| --- | --- |
| Starfield | Stars travel outward from the center. |
| Rain | Blue streaks fall across the screen. |
| Nebula | A slowly moving plasma field speeds up with the audio level. |
| Now Spinning | Album art rotates inside a disc. Background bars follow stereo audio levels. The disc stops while paused. |
| Tunnel | Wireframe rings move toward the viewer and react to audio levels. |
| Toasty Squadron | The existing C PNG sprite sequences fly through a layered formation. |
| Off | A plain background. Meters have a separate setting. |

These are Go implementations of the C client's background choices. Their motion and layouts are not pixel-identical ports. Nebula and the spinning bars currently use stereo RMS levels, not the C waveform or frequency analysis. Toasty reuses the existing asset files, resized during loading, with Go's movement and layering.

Default Toasty assets are found under `assets/toasty` beside the configuration file, `assets/toasty` in the working directory, `/media/fat/misterfin-crt/toasty`, or the legacy `/media/fat/misterfin/toasty` directory. The default cycle omits Toasty when those assets are absent, including when a partial configuration changes only `show_audio_meters` or `default_background`. Explicitly configured missing assets produce a configuration error.

## Configuration

Music appearance settings belong in the `music_visuals` section of `settings.json` beside `jellyfin.conf`. This section controls backgrounds and stereo level meters during music playback. It does not change volume or playback controls.

The [shared example](../settings.example.json) selects Starfield and enables meters. Omitted fields retain their defaults. Settings load at startup. The legacy `MISTERFIN_MUSIC_CONFIG` override still accepts a separate file and takes precedence over this section.

| Setting | Meaning |
| --- | --- |
| `default_background` | Name of the background selected at startup. Defaults to `Starfield`. Must appear in `backgrounds`. |
| `show_audio_meters` | Show or hide stereo level meters. Defaults to `true`. |
| `backgrounds` | Ordered selection cycle, containing 1 to 16 presets. |
| `name` | Unique preset label, 1 to 32 characters. |
| `type` | `none`, `starfield`, `rain`, `nebula`, `spinning`, `tunnel`, `sprites`, or `image`. |
| `speed` | Animation speed multiplier, 0.1 to 4. Defaults to 1. |
| `density` | Particle count, 1 to 128. Defaults to 40. Sprite formations use at most 15. |
| `intensity` | Background brightness, 0 to 1. Defaults to 0.65. Album art remains at normal brightness. |
| `color` | Optional hexadecimal RGB color. Applies to stars, rain, plasma, tunnel lines, and spinning bars. |
| `files` | Image or animation paths for `image` and `sprites`, relative to `settings.json` unless absolute. A legacy override resolves beside its own file. |
| `fps` | Frame rate for a sequence of still images, 1 to 60. Defaults to 12. Animated GIFs use their own frame delays. `speed` scales both. |

For example, this configuration uses a custom GIF and a slower green starfield:

```json
{
  "music_visuals": {
    "default_background": "My animation",
    "show_audio_meters": true,
    "backgrounds": [
      {
        "name": "My animation",
        "type": "image",
        "files": [
          "music/night.gif"
        ],
        "intensity": 0.5
      },
      {
        "name": "Green stars",
        "type": "starfield",
        "color": "#70dd90",
        "speed": 0.5,
        "density": 24
      },
      {
        "name": "Off",
        "type": "none"
      }
    ]
  }
}
```

An `image` preset fits PNG, JPEG, or GIF artwork to the display without stretching its aspect ratio. A `sprites` preset repeats its supplied sequence across moving sprites. Multiple paths play in the listed order. Users can change artwork, palettes, speed, and selection order without rebuilding. A new procedural effect requires a Go implementation of `musicviz.Effect` and registration in the effect factory.

Startup validates settings and passes immutable presets to the browser. Images and sprite sequences decode on first selection in a worker, with a loading message. Decoded assets remain cached for the application session. Loading runs outside the browser event loop.

Files must be no larger than 4 MiB, with dimensions no larger than 1024 pixels per axis. Custom presets must contain at most 128 decoded frames. A GIF must also fit within 32 MiB before resizing. The complete decoded asset library is limited to 32 MiB. Background images are reduced to at most 640 pixels on their longest side. Sprites are reduced to 96 pixels.

Invalid configuration disables music backgrounds for that run and queues a brief settings notice. Ordinary music playback remains available. A custom asset that fails on selection shows “Background unavailable. Check music assets.” The background selection control remains available to choose another preset.

The old section name `music` and field names `default` and `meters` remain accepted. Use the new names in new configurations. The new fields take precedence over the old fields. An explicit `null` in a new field selects its default. If both section names exist, `music_visuals` takes precedence as a whole. Migration writes the new names.

## Audio and rendering

MiSTer's MPlayer exports a small planar PCM sample window to a unique temporary file. The playback loop reads it at 20 Hz and computes normalized stereo RMS amplitudes. Ghostty's libmpv audio helper reads `astats` measurements through the complete `af-metadata/music` node and publishes the same normalized values. These interfaces follow the [mpv metadata property contract](https://mpv.io/manual/master/#command-interface-af-metadata/%3Cfilter-label%3E) and [FFmpeg astats metadata](https://ffmpeg.org/ffmpeg-filters.html#astats). The fallback FFplay decoder has no meter feedback, so its meters stay empty and backgrounds keep their idle animation.

Audio measurements are disposable and never block decoding. The browser rejects measurements from old decoder generations. Pausing, stopping, or losing fresh measurements makes the meters decay toward silence. Meters use a visual -48 dB floor and fixed green/yellow/red zones. They do not adjust volume and are not calibrated analog VU instruments.

The browser puts immutable presets, the selected index, and copied audio levels into `Scene`. `RasterRenderer` owns `musicviz.Renderer`, which advances one `Effect` using elapsed time and draws into the shared UI canvas. Each effect has its own file and animation state. The screen painter owns text, artwork placement, progress, and controls. Output adapters receive finished pixels. Effects contain no filesystem, decoder, or output-backend logic.
