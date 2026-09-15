# Music and visual backgrounds

Selecting a track starts album playback and advances in order, including across pages. Shoulders or brackets change tracks immediately. Triggers or J/L seek ten seconds. Any direction toggles controls. Open pauses/resumes without instructions, and Back returns to the track list. See [playback controls](GO_PLAYBACK.md#playback-controls).

## Whole-library shuffle

On a music library's artist list, SELECT/Tab starts shuffle. Jellyfin supplies batches of up to 64 random tracks. A new batch loads when needed. Tracks can repeat between batches, but immediate repetition is avoided when another track is available. The client retains at most two batches of history. Back cancels pending loading and restores the artist selection.

Remote queues also support shuffle and repeat. See [remote control](GO_REMOTE.md). These queue states are separate from visual settings.

## Built-in backgrounds

SELECT/Tab cycles backgrounds during music playback and briefly shows the name. The choice lasts across tracks for the current application session. Restarting selects the configured default.

| Background | Behavior |
| --- | --- |
| Starfield | Stars travel outward. |
| Rain | Falling blue streaks. |
| Nebula | Plasma motion responds to audio level. |
| Now Spinning | Rotating album art and stereo-reactive bars. The disc stops when paused. |
| Tunnel | Audio-reactive wireframe rings. |
| Toasty Squadron | Layered flying sprite sequences. |
| Off | Plain background. Meters remain separately configurable. |

Effects use the shared Go renderer and do not reproduce every detail of the C visualizers. The default cycle omits Toasty when its optional assets are absent. Asset discovery checks `assets/toasty` beside the configuration, then in the working directory, `/media/fat/misterfin-crt/toasty`, and the legacy `/media/fat/misterfin/toasty` directory.

## Configuration

`music_visuals` in `settings.json` controls appearance, not volume or playback bindings. Omitted fields use defaults. Settings load at startup. See [configuration](GO_CONFIGURATION.md) for legacy aliases and overrides.

| Field | Default and limits |
| --- | --- |
| `default_background` | `Starfield`. Must name a preset in the cycle. |
| `show_audio_meters` | `true`. |
| `backgrounds` | Built-in cycle. A custom cycle contains 1–16 presets. |
| Preset `name` | Unique label, 1–32 characters. |
| `type` | `none`, `starfield`, `rain`, `nebula`, `spinning`, `tunnel`, `sprites`, or `image`. |
| `speed` | 1. Range 0.1–4. |
| `density` | 40. Range 1–128. Sprite formations use at most 15. |
| `intensity` | 0.65. Range 0–1. Does not dim album art. |
| `color` | Optional hexadecimal RGB color for procedural effects. |
| `files` | Paths for image/sprite presets, relative to the settings file unless absolute. |
| `fps` | 12. Range 1–60 for still-image sequences. GIFs use their own delays. |

```json
{
  "music_visuals": {
    "default_background": "Night",
    "show_audio_meters": true,
    "backgrounds": [
      {"name": "Night", "type": "image", "files": ["music/night.gif"], "intensity": 0.5},
      {"name": "Green stars", "type": "starfield", "color": "#70dd90", "speed": 0.5, "density": 24},
      {"name": "Off", "type": "none"}
    ]
  }
}
```

Image presets fit PNG, JPEG, or GIF without stretching. Multiple paths play in order. Sprite presets repeat their sequence across moving sprites. Video backgrounds are not supported. A new procedural effect requires a `musicviz.Effect` implementation.

Files are limited to 4 MiB and 1024 pixels per axis. A preset allows at most 128 decoded frames. GIFs must fit within 32 MiB before resizing, and the full decoded asset library is limited to 32 MiB. Backgrounds shrink to at most 640 pixels on the longest side, sprites to 96 pixels.

Assets load on a worker at first selection and remain cached for the session. Invalid settings disable backgrounds with a notice. A failed custom asset displays “Background unavailable. Check music assets.” Another preset can still be selected, and music remains playable.

## Audio meters

MPlayer exports a small PCM window sampled at 20 Hz. The Python/libmpv helper supplies stereo RMS measurements. FFplay has no meter feedback. Measurements never block decoding, and stale values decay toward silence. Meters use a visual −48 dB floor with green/yellow/red zones. They do not adjust volume and are not calibrated analog VU instruments.

`RasterRenderer` owns `musicviz.Renderer` and its effect state. The browser supplies immutable presets and copied levels. File loading, decoding, and output selection remain outside effects. See [architecture](GO_RENDERING.md).
