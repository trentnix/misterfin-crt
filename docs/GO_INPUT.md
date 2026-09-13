# Go input configuration

Controller bindings are configurable without rebuilding. The default layout uses the D-pad to toggle playback controls, shoulders to change music tracks, and triggers to seek. See [the playback guide](GO_PLAYBACK.md) for the default controller and keyboard controls.

## Configuration path

Copy [input.json.example](../input.json.example) to `input.json` beside your `jellyfin.conf`, then edit the profile for your controller. The MiSTer test launcher uses `/media/fat/misterfin/input.json`. The configuration persists across application restarts.

Use `-input-config /path/to/input.json` to choose another file. The `MISTERFIN_INPUT_CONFIG` environment variable supplies the flag's default and also works through the Ghostty harness. A missing default file keeps the built-in layout. An explicitly selected file must exist. Invalid configuration stops startup with an error that names the file. Restart the application after editing the configuration.

Profiles configure Linux hardware input, including controllers and physical keyboards on MiSTer. Ghostty reads terminal key sequences and keeps the keyboard bindings documented in the playback guide. On-screen button hints describe the default layout.

## Device profiles

Each profile's `match` is a case-sensitive glob for the Linux input device name. For example, `*Xbox*` matches `Microsoft Xbox Controller`. Read `/proc/bus/input/devices` to find device names. Device names remain useful when event numbers change after reconnection. Profiles apply to hotplugged devices too.

Matching profiles apply in file order. Omitted inputs retain the built-in bindings. Set an action to `""` to disable one input. Set `"replace": true` to clear all inherited bindings for a matching device before applying the profile. A replacement profile must include every input you want to use, including navigation and Back.

The `buttons` object maps decimal Linux `EV_KEY` codes to actions. These codes come from the controller driver, not the labels printed on the controller. For example, Xbox shoulder buttons usually report 310 and 311. To make those buttons seek instead of changing tracks:

```json
{
  "profiles": [
    {
      "match": "*Xbox*",
      "buttons": {
        "310": "seek-backward",
        "311": "seek-forward"
      }
    }
  ]
}
```

Use an input-event inspector such as `evtest` on Linux to identify a controller's button codes, axis codes, and ranges. An input device name alone does not guarantee identical codes across different drivers.

MiSTer's synthetic action-key echoes remain filtered to prevent duplicate presses. Virtual arrow events remain available for controllers that depend on MiSTer routing. If you remap physical directions, apply the same direction mappings to `MiSTer virtual input`, or disable that virtual device with a matching replacement profile if your controller supplies all directions directly.

## Analog axes

The `axes` object maps decimal Linux `EV_ABS` codes. Each axis can have `negative` and `positive` actions. An empty object disables that axis. Axis configuration overrides built-in hat and trigger mappings.

The driver supplies the minimum and maximum values. Choose `rest` according to the input:

| Rest | Use | Available actions |
| --- | --- | --- |
| `center` (default) | Stick or directional hat | `negative` and `positive` |
| `minimum` | Trigger resting at its minimum | `positive` |
| `maximum` | Trigger resting at its maximum | `negative` |

For example, this profile adds stick navigation and uses an inverted trigger for forward seeking:

```json
{
  "profiles": [
    {
      "match": "My Controller",
      "axes": {
        "0": { "negative": "previous", "positive": "next", "press": 40, "release": 25 },
        "1": { "negative": "up", "positive": "down", "press": 40, "release": 25 },
        "5": { "rest": "maximum", "negative": "seek-forward" }
      }
    }
  ]
}
```

`press` and `release` are percentages of travel from rest toward the chosen end. Defaults are 25 and 15. Release must be greater than zero and less than press. Press must be at most 100. Separate thresholds prevent small fluctuations from producing repeated button presses. Unsupported axes produce no actions.

## Actions

Bindings describe intent. The browser decides how an action behaves in the current screen. Repeat timing follows the action, regardless of which physical input produces it.

| Action | Behavior |
| --- | --- |
| `up`, `down` | Navigate lists. Any direction toggles the controls during music or video playback. |
| `previous`, `next` | Navigate the carousel, browse pages, or photos. Toggle controls during music or video playback. |
| `track-previous`, `track-next` | Change music tracks. Navigate pages or photos outside playback. |
| `seek-backward`, `seek-forward` | Seek music by 10 seconds or library video by 30 seconds. Live TV ignores seeking. |
| `open` | Open the selection or pause/resume playback. |
| `back` | Back out or stop playback. |
| `select` | Restart a resumable video from its selection screen. |
| `retry` | Retry the current failed request. |
| `quit` | Exit the application. |

Held navigation accelerates. Held seeking repeats at a steady rate. Playback menu toggles and music track changes happen once per press, including when assigned to an analog axis.
