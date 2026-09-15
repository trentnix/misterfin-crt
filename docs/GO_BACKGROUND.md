# Browsing background

Set the `background` section in `settings.json` beside the `jellyfin.conf` used by your launcher:

```json
{
  "background": {
    "image": "background.png"
  }
}
```

Place the image in that directory and restart MiSTerFin CRT. Relative paths resolve beside `settings.json`. Absolute paths also work. For the ordinary MiSTer installation, the directory is `/media/fat/misterfin-crt`. The current 480i launcher uses `/media/fat/misterfin-crt/interlaced-test`. For local development, use the folder containing the configuration passed to the harness.

One static image replaces the background on the library carousel and every browsing list, including Continue Watching and music lists. Item posters remain visible. Details, About, setup, photo viewing, video playback, and music playback retain their existing presentation. Music playback backgrounds are configured in the `music_visuals` section.

PNG and JPEG are supported, up to 4 MiB and 2048 pixels in either dimension. A 4:3 image such as 640×480 is a good fit. The renderer preserves the image’s proportions, fills the physical 4:3 screen, and crops excess width or height from the center. It accounts for the tall logical pixels used by the CRT interface. Transparent pixels appear over black. The image is dimmed to keep labels readable.

If the `background` section is omitted, contains `{}`, or has an empty `image`, the client uses its normal backgrounds: mosaics on the carousel and item artwork on lists. If the section has invalid values or the image cannot be read or decoded, the client uses the normal artwork and shows “Custom background unavailable. Using normal artwork.” for four seconds once browsing is ready. A background problem does not prevent startup. Correct the setting and restart to try the custom image again.

The client decodes the image once during startup. The shared renderer caches its scaled and dimmed pixels. It performs no background file reads or repeated image scaling while navigating. Hidden mosaic and list-backdrop downloads are skipped while the custom image is configured. Library counts and visible posters still load. MiSTer and Ghostty use the same scene and rendering code.

## Browsing title

Set `ui.title` in `settings.json` to change the heading on the carousel and root library list:

```json
{
  "ui": {
    "title": "Trent's CRT"
  }
}
```

Restart the application to apply the title. If the `ui` section or its `title` is omitted, the heading defaults to `MiSTerFin CRT`. Set `"title": ""` to hide the heading while keeping the clock. Whitespace-only titles also hide the heading. Whitespace is collapsed to single spaces and control characters are removed. Long titles end with `...` and stay within the heading area, leaving room for the clock and CRT overscan. At the standard 640-pixel width, the heading fits 33 characters, including the ellipsis. Individual library and item titles keep their existing scrolling behavior. The About page and application name remain `MiSTerFin CRT`.

The `ui` section must be an object, at most 4 KiB, with only the `title` key. Invalid section values restore `MiSTerFin CRT` with a brief settings notice. Settings are not rewritten. See [configuration defaults and migration](GO_CONFIGURATION.md) for file-level errors and legacy files.
