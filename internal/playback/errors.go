package playback

import "errors"

// ErrStartupTimeout means the decoder reported no playback position within
// the startup deadline. Callers can use errors.Is to present recovery guidance.
var ErrStartupTimeout = errors.New("player did not start playback within 30 seconds")

// ErrNotStarted means the decoder exited before reporting playback progress.
var ErrNotStarted = errors.New("player could not start playback")

// ErrInterrupted means the decoder failed after playback had started.
var ErrInterrupted = errors.New("player interrupted playback")

// ErrProgress means server progress reporting failed independently of decoding.
var ErrProgress = errors.New("server playback progress reporting failed")

// ErrPlayerUnavailable means no usable decoder executable is installed or configured.
var ErrPlayerUnavailable = errors.New("media player is unavailable")

// ErrTrackUnavailable means a selected audio or subtitle track is no longer present.
var ErrTrackUnavailable = errors.New("selected media track is unavailable")
