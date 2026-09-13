# MiSTerFin Go port plan

## Status and provenance

This repository starts from the working C implementation in `/mnt/Data/Source/MiSTerFin-trentnix`, branch `local-all-features`, commit `19d99fa5f479692e45ea7b5dddc42e42fb1782a9` (Merge clean pause controls into local integration). The C integration repository preserves that starting point. The source path is recorded here to identify the local integration repository, whose merged branch may not be published.

The original project is [Pudding Studio's MiSTerFin](https://github.com/puddingstudio/MiSTerFin). The integration repository is [trentnix/MiSTerFin](https://github.com/trentnix/MiSTerFin). Git history preserves the ancestry and contributor records. The local `source` remote points to the integration repository. The Go repository publishes separately as `trentnix/misterfin-crt`.

The Go implementation now includes a framebuffer prototype and a Ghostty browsing path with configuration, authentication, paginated lists, and artwork. The C source and tests in the MiSTerFin integration repository remain the behavioral reference. The working tree contains only the Go application and its native dependencies. Inspect the original application in the MiSTerFin integration repository at the recorded commit. External-player video playback uses one Go overlay renderer and output-specific composition for headless decoder frames and native `mplayer-arm`. See [the playback record](GO_PLAYBACK.md), [the build record](GO_BUILD.md), and [the browsing record](GO_BROWSING.md) for validation and remaining limits. Ignored configuration, tokens, build products, and other untracked files were not copied by the clone.

## Objective

Move Jellyfin communication and experience development into Go while preserving the MiSTer behavior established by the C implementation. Validate the Go/C boundary with a small prototype before porting the full application.

## Proposed ownership

| Component | Owner |
| --- | --- |
| Jellyfin HTTP, JSON, authentication, and session reporting | Go |
| Navigation, pagination, asynchronous loading, and application state | Go |
| Drawing and animation | Go |
| Launching and controlling the external player | Go |
| Framebuffer setup, mapping, flipping, and vsync | Small C platform layer |
| MiSTer DDR integration and evdev input | Small C platform layer |
| Media decoding and playback | Existing external `mplayer-arm` process |

The platform API must define buffer ownership, lifetime, input events, errors, and shutdown. Keep C calls coarse enough to avoid crossing the language boundary for each pixel. Audit the current framebuffer and input code before choosing the exact API. Use an interface that allows API and navigation tests to run without C or hardware.

## Milestones

1. Establish the build and platform boundary. Verify a supported Go toolchain against the actual MiSTer kernel and ARM environment. Reuse the existing Zig target configuration if cgo compatibility checks pass. Add a Go entry point and a small C adapter that can present a test frame in the headless harness and on MiSTer. Record reproducible host and ARM build commands.
2. Implement one complete browsing path. Add configuration loading, Jellyfin authentication, root libraries, and item lists. Preserve the merged branch's collection-specific queries, pagination, cancellation behavior, and error handling. Render text and images in Go. Exercise the same navigation in Ghostty and on MiSTer.
3. Evaluate the prototype. Measure frame time, memory use, binary size, and input latency against the C baseline. Test slow and failed requests, rapid navigation, and large libraries. Decide whether the platform boundary is suitable before expanding the port.
4. Add playback. Keep `mplayer-arm` external. Port process control, session lifecycle, pause, seek, resume, and framebuffer handoff. Include the merged Live TV behavior and audio/video synchronization checks.
5. Complete feature coverage and packaging. Track remaining C features explicitly. Provide separate executable names and installation paths for testing alongside the C client. Review updater URLs, release workflows, asset paths, and deployment scripts before publishing or deploying a Go build.

The first implementation milestone ends with a test frame on both the desktop and MiSTer. The first useful product milestone ends with browsing on both platforms. Full playback is a later milestone.

## Validation

Keep the C implementation available as a reference until each replacement works. Use focused Go tests for request construction, response handling, and navigation transitions. Use the existing C tests where relevant. Validate native hardware behavior on MiSTer because host tests cannot establish framebuffer timing or DDR correctness.

No application tests or ARM builds were run as part of repository setup. Setup changes only identify the adaptation and record this plan.

## Licensing and attribution

Retain [the original license](../LICENSE), existing copyright notices, and [third-party notices](THIRD_PARTY.md). Describe copied and translated material as derived from MiSTerFin. Third-party components retain their own terms, including the patched MPlayer component's GPL terms.

The initial licensing approach is to retain CC BY-NC 4.0 for MiSTerFin-derived material and use the same license for adaptation contributions. Any future licensing change must account for the rights in inherited material. Use a distinct project identity and make independent maintenance clear.
