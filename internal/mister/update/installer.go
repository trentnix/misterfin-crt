// Package update installs verified release bundles into a MiSTer installation.
// Downloads and filesystem transactions are independent of browsing and rendering.
package update

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"mistervision/internal/release"
	updateapi "mistervision/internal/update"
)

const pendingName = ".update-pending"

var versionPattern = regexp.MustCompile(`^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$`)

// Installer owns fixed application and launcher destinations. It never changes
// active settings, sign-in, preferences, caches, or the optional interlaced core.
// The caller must stop playback before Install and exit after success.
type Installer struct {
	root, launcher string
	client         *http.Client
	// replace is the atomic file operation, replaceable by fault-injection tests.
	replace func(string, string, os.FileMode) error
}

// New configures an existing installation. Paths must be absolute. No I/O occurs
// until Install or Recover. Each operation locks the installation directory.
func New(root, launcher string) *Installer {
	return &Installer{root: filepath.Clean(root), launcher: filepath.Clean(launcher), client: downloadClient(), replace: replaceFile}
}

// Install downloads a bounded release bundle, verifies every file, and commits
// the pair with rollback copies. Existing release output is never executed.
func (i *Installer) Install(ctx context.Context, status release.Status, notify func(updateapi.Progress)) error {
	if !status.Available || !status.HasBundle || !versionPattern.MatchString(status.Latest) {
		return updateapi.ErrManual
	}
	unlock, err := i.lock()
	if err != nil {
		return err
	}
	defer unlock()
	if restored, err := i.recover(); err != nil {
		return err
	} else if restored {
		// Another process may have interrupted an update after our startup.
		// The running client must not continue with a newly restored player.
		return updateapi.ErrRecovery
	}
	// A committed transaction whose cleanup failed must not be overwritten.
	if _, err := os.Lstat(filepath.Join(i.root, pendingName)); !errors.Is(err, os.ErrNotExist) {
		return errors.New("previous update files could not be removed")
	}
	stage, err := os.MkdirTemp(i.root, ".update-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	report := func(p updateapi.Progress) {
		if notify != nil {
			notify(p)
		}
	}
	work, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	report(updateapi.Progress{Phase: updateapi.Downloading})
	archive, err := i.download(work, stage, status, report)
	if err != nil {
		return err
	}
	report(updateapi.Progress{Phase: updateapi.Validating})
	entries, err := i.unpack(work, stage, archive, status.Latest)
	if err != nil {
		return err
	}
	if err := i.backup(work, stage, entries); err != nil {
		return err
	}
	if err := work.Err(); err != nil {
		return err
	}
	if err := writeJournal(stage, entries); err != nil {
		return err
	}
	pending := filepath.Join(i.root, pendingName)
	if err := os.Rename(stage, pending); err != nil {
		return err
	}
	if err := syncDir(i.root); err != nil {
		return i.failed(err)
	}
	report(updateapi.Progress{Phase: updateapi.Installing})
	for index, entry := range entries {
		if err := work.Err(); err != nil {
			return i.failed(err)
		}
		if err := i.replace(i.destination(entry.Name), filepath.Join(pending, fmt.Sprintf("new-%d", index)), mode(entry.Name)); err != nil {
			return i.failed(err)
		}
	}
	if err := work.Err(); err != nil {
		return i.failed(err)
	}
	marker := filepath.Join(pending, "committed")
	commitErr := durableWrite(marker, []byte("1\n"), 0600)
	if commitErr == nil {
		commitErr = syncDir(pending)
	}
	if commitErr != nil {
		// A failed marker write can still leave its full contents behind.
		// Remove it before recovery so rollback cannot mistake it for success.
		if err := os.Remove(marker); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.Join(updateapi.ErrRecovery, commitErr, err)
		}
		return i.failed(commitErr)
	}
	// Failure to remove committed backups does not undo a successful install.
	// Recover retries their cleanup at the next startup.
	_ = os.RemoveAll(pending)
	return nil
}

// Recover restores an interrupted transaction before any display or decoder is
// opened. A true result means the caller must re-exec the restored client before
// proceeding, because the currently running executable may be the replaced one.
func (i *Installer) Recover() (bool, error) {
	unlock, err := i.lock()
	if err != nil {
		return false, err
	}
	defer unlock()
	return i.recover()
}

func (i *Installer) failed(cause error) error {
	if _, err := i.recover(); err != nil {
		return errors.Join(cause, updateapi.ErrRecovery, err)
	}
	return cause
}
