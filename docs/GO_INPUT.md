# Controller configuration

Set device profiles in the `input` section of `settings.json`. Omission or `{"profiles": []}` keeps built-in bindings. Invalid input settings stop startup. Restart after editing. See [settings paths and overrides](GO_CONFIGURATION.md#paths-and-precedence) and [default playback controls](GO_PLAYBACK.md#playback-controls).

Profiles configure Linux evdev devices, including MiSTer controllers and physical keyboards. Ghostty uses terminal key sequences instead. Hints follow the last physical device used, while terminal input supplies keyboard labels.

The default controller layout follows MiSTer: B selects, plays, or pauses and A returns, cancels, or stops. Explicit button bindings override these defaults. Keyboard controls remain Enter to select and Escape to return.

## Profiles and labels

`match` is a case-sensitive glob for the Linux device name. See [finding device names and button codes](#finding-device-names-and-button-codes) to identify the controller and its inputs. Matching profiles apply in file order, including after hotplug.

Omitted bindings retain defaults. An empty action disables one binding. `replace: true` clears all inherited bindings first, so a replacement profile must provide every needed action. Buttons use decimal Linux `EV_KEY` codes, which depend on the driver rather than the printed controller labels.

```json
{
  "input": {
    "profiles": [
      {
        "match": "*Xbox*",
        "buttons": {
          "310": "track-previous",
          "311": "track-next"
        },
        "button_labels": {
          "310": "LB",
          "311": "RB"
        },
        "axes": {
          "0": {
            "negative": "previous",
            "positive": "next",
            "press": 40,
            "release": 25
          },
          "1": {
            "negative": "up",
            "positive": "down",
            "press": 40,
            "release": 25
          },
          "2": {
            "rest": "minimum",
            "positive": "seek-backward"
          },
          "5": {
            "rest": "minimum",
            "positive": "seek-forward"
          }
        },
        "axis_labels": {
          "2": {
            "positive": "LT"
          },
          "5": {
            "positive": "RT"
          }
        }
      }
    ]
  }
}
```

Common codes have built-in names. Unknown inputs display labels such as `Btn 288` or `Axis 4+`. Custom labels allow at most 12 printable ASCII characters. Empty labels restore built-in names. Labels follow their physical inputs when actions change. Badges prefer explicit bindings, omit disabled actions, and wrap within the CRT safe area.

MiSTer's synthetic action-key echoes are filtered to avoid duplicate presses. Virtual arrows remain available. If remapping physical directions, apply the same mappings to `MiSTer virtual input`, or disable that virtual device with a replacement profile when physical input supplies every direction. Virtual echoes do not replace the physical device's labels.

## Finding device names and button codes

The keys in `buttons` are decimal Linux `EV_KEY` codes, not controller button numbers assigned by MiSTer's menu. For a standard Xbox mapping:

| Button | Linux event name | Configuration key |
| --- | --- | --- |
| A | `BTN_SOUTH` | `"304"` |
| B | `BTN_EAST` | `"305"` |

These codes come from the [Linux input definitions](https://github.com/torvalds/linux/blob/master/include/uapi/linux/input-event-codes.h). The physical button associated with each code depends on the controller and driver.

To inspect a controller on the device running MiSTerVision:

1. Exit MiSTerVision. Read `/proc/bus/input/devices` to find the controller's name and its `eventN` handler. Use the physical controller, not `MiSTer virtual input`.
2. If `evtest` is installed, run `evtest /dev/input/eventN` as root, replacing `eventN` with that handler. On a desktop Linux system, use `sudo evtest /dev/input/eventN`.
3. Press the button. An event containing `type 1 (EV_KEY), code 304 (BTN_SOUTH), value 1` identifies a press of code `304`. Use `"304"` as the configuration key. The device number in `eventN` and the event's `value` are not button codes.
4. Press Ctrl+C to stop. Set `match` to the reported device name or a matching glob, save the profile, and restart MiSTerVision.

For sticks and triggers, inspect `EV_ABS` events and put their codes in `axes`. A controller can expose triggers as buttons or axes. Codes observed on a desktop may differ from MiSTer if the driver or controller connection mode differs.

MiSTerVision does not include a button-identification tool. If `evtest` is unavailable, identifying unknown inputs requires another Linux input inspector. See the [Linux input documentation](https://docs.kernel.org/input/input.html) for background on event devices and `evtest`.

## Axes

`axes` maps decimal Linux `EV_ABS` codes. The driver supplies each range. An empty axis object disables that axis.

Gamepads use the left stick for navigation by default: axis `0` selects left/right, and axis `1` selects up/down. A direction activates at 40% travel and releases below 25%, preventing small stick movements from navigating. Held directions use the same acceleration as the D-pad. Explicit axis bindings override these defaults, and `replace: true` removes them. The right stick is unmapped unless configured.

| `rest` | Input | Allowed directions |
| --- | --- | --- |
| `center` (default) | Stick or hat | `negative`, `positive` |
| `minimum` | Trigger resting at minimum | `positive` |
| `maximum` | Trigger resting at maximum | `negative` |

`press` and `release` are percentages of travel from rest. Defaults are 25 and 15. Release must be greater than zero and less than press. Press must be at most 100. Separate thresholds prevent small fluctuations from repeating actions. Unsupported axes produce no actions.

## Actions

| Action | Meaning |
| --- | --- |
| `up`, `down` | Select rows. Toggle controls during video/music. |
| `previous`, `next` | Change home cards, jump list screens, or navigate photos. Toggle controls during video/music. |
| `track-previous`, `track-next` | Change music tracks. Navigate list screens/photos outside playback. |
| `seek-backward`, `seek-forward` | Seek music by 10 seconds or recorded video by 30 seconds. Ignored for Live TV. |
| `open` | Open a selection, apply a choice, or pause/resume. |
| `back` | Return, dismiss, cancel, or stop playback. |
| `about` | Toggle About while browsing. Default: START/Menu or F1. |
| `select` | Switch the home view, restart resumable video from details, open video options, start library shuffle, or cycle music backgrounds. |
| `retry` | Retry or refresh the current request. |
| `quit` | Exit the application. |

Context determines the action. In video options, directions navigate tabs/rows and seek inputs adjust client-text subtitle timing. Applying a successful choice dismisses the picker. See [video options](GO_PLAYBACK.md#video-options).

Held navigation starts repeating after 350 ms, uses six 110 ms intervals, then accelerates to 45 ms. Held seeks repeat every 250 ms after 350 ms. Menu toggles and track changes act once per press. Supporting terminals report presses, repeats, and releases through the Kitty keyboard protocol. Legacy terminal input cannot distinguish held repeats from repeated presses.

[`control.Action`](../internal/input/control/action.go) defines and validates semantic actions. The input readers own physical mapping and repeat timing. Renderers receive resolved labels and perform no device or configuration I/O.
