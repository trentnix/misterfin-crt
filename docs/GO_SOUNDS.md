# Navigation sounds

Browsing uses the inherited MiSTerFin navigation and confirmation clips at a quieter default volume. Selection changes produce a short click. Opening or returning from a screen, dismissing a notice, and switching browsing modes produce a confirmation sound. Reaching a list boundary, redraws, and background artwork updates stay silent. Video and music playback, including their control menus, stay silent as in the C client.

## Configuration

Place `sounds.json` beside `jellyfin.conf`. On MiSTer, the default location is `/media/fat/misterfin-crt/sounds.json`. On the desktop, it is beside the Jellyfin configuration passed to the harness. Copy [sounds.example.json](../sounds.example.json) to start with the defaults:

```json
{
  "enabled": true,
  "volume": 10
}
```

`volume` ranges from 0 to 100 and scales the original clip amplitude. The default of 10 is about 14 dB below the C client's 50% gain. It does not change media volume or the system mixer. Set `enabled` to `false` to disable feedback, or set `volume` to 0. Restart the application after changing the file.

A missing file uses the defaults. Omitted fields retain their defaults. Unknown keys, malformed JSON, and out-of-range volume values produce a configuration error at startup. Local `sounds.json` is ignored by Git.

The Go executable accepts `-sound-config /path/to/sounds.json`. `MISTERFIN_SOUND_CONFIG` selects the same override and also works with the Ghostty harness. An explicit command-line flag takes precedence over the environment variable.

## Audio ownership

The shared browser emits semantic cues through [`sound.Feedback`](../internal/sound/sound.go). The sound player keeps one pending cue and never waits on the UI loop. One mutex protects pending audio and device access. If the worker is busy, navigation skips the cue. Playback suspension clears pending audio under the same lock, so clicks cannot survive a playback handoff. It embeds the existing clips, scales them once at startup, and reuses the device during short bursts. Rapid navigation cannot leave a queue of clicks playing after scrolling stops.

Both Linux targets use the small [`alsa`](../internal/sound/alsa/alsa_linux.go) adapter. It loads `libasound.so.2` at runtime and writes stereo 48 kHz PCM without opening a process per click. The adapter requires no ALSA development library in the ARM toolchain. Missing or busy devices temporarily suppress feedback. Builds without Linux/cgo remain usable without UI sounds.

Before any media player starts, its worker suspends feedback, discards queued sounds, and closes the audio device. The worker releases that suspension after the player finishes or launch fails. Overlapping playback attempts retain suspension until all attempts release it. This applies to both music and video because MiSTer's audio device cannot be shared reliably. The sound worker sleeps when inactive and does no rendering work.

## Validation

Tests cover configuration, clip format and gain, disabled feedback, device failures, idle cleanup, overlapping playback suspension, failed decoder launches, selection changes, and silent playback controls. Native adapter tests use ALSA's null device. Browser integration fixtures disable sounds so automated runs do not play audio on the workstation. Audibility and preferred volume still require listening on the target speakers.
