// Package update defines the installation boundary used by the shared browser.
// Output backends never download releases or replace application files.
package update

import (
	"context"
	"errors"

	"mistervision/internal/release"
)

// ErrManual means the release cannot be installed by this updater protocol.
var ErrManual = errors.New("this release requires manual installation")

// ErrRecovery means installation failed and rollback needs another startup attempt.
// The caller must exit instead of allowing playback with a possibly mixed pair.
var ErrRecovery = errors.New("restart required to recover the previous installation")

// Phase identifies work that can be shown without exposing paths or network errors.
type Phase uint8

const (
	// Downloading transfers the checksums and archive to temporary storage.
	Downloading Phase = iota
	// Validating verifies archive contents and prepares rollback copies.
	Validating
	// Installing replaces the validated files. Cancellation still rolls back.
	Installing
)

// Progress is a copied installation snapshot. Total is zero when size is unknown.
// Received and Total count downloaded archive bytes, not extracted bytes.
type Progress struct {
	Phase           Phase
	Received, Total int64
}

// Installer applies a release on a supported installation target. Install must
// honor cancellation and finish rollback before returning a cancellation error.
// notify is optional and called serially on the worker. It must return promptly.
// After success, the caller must exit before starting another media player.
// A supported launcher can then start the updated application.
type Installer interface {
	Install(context.Context, release.Status, func(Progress)) error
}
