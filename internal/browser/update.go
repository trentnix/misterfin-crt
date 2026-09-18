package browser

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"syscall"
	"time"

	"mistervision/internal/diagnostics"
	"mistervision/internal/update"
)

// updateWork owns the installer worker. Shutdown joins it after cancellation so
// no filesystem transaction outlives the application or display teardown.
type updateWork struct {
	cancel  context.CancelFunc
	done    <-chan struct{}
	exitAt  time.Time
	exitErr error
}

func (s *browserSession) installUpdate() {
	if s.config.Updater == nil || !s.about.Release.HasBundle || s.about.ManualInstall || s.about.Updating {
		return
	}
	if s.controller.running || s.media.pending || s.model.MusicQueueActive() {
		s.about.Message = messageUpdateStopPlayback
		return
	}
	s.stopRemote()
	s.about.Updating = true
	s.about.Progress = update.Progress{Phase: update.Downloading}
	s.about.Message = ""
	installer, status := s.config.Updater, s.about.Release
	ctx, cancel := context.WithCancel(s.ctx)
	done := make(chan struct{})
	s.update.cancel, s.update.done = cancel, done
	s.config.Diagnostics.Record("update.start")
	go func() {
		defer close(done)
		defer cancel()
		err := installer.Install(ctx, status, func(progress update.Progress) {
			s.send(s.ctx, updateProgressResult{progress})
		})
		s.send(s.ctx, installResult{err})
	}()
}

type updateProgressResult struct{ progress update.Progress }

func (r updateProgressResult) apply(s *browserSession) bool {
	s.about.Progress = r.progress
	return true
}

type installResult struct{ err error }

func (r installResult) apply(s *browserSession) bool {
	s.about.Updating = false
	s.config.Diagnostics.Record("update.end", slog.Bool("failed", r.err != nil), slog.Bool("canceled", errors.Is(r.err, context.Canceled)), slog.Bool("recovery_required", errors.Is(r.err, update.ErrRecovery)), slog.String("error_kind", diagnostics.ErrorKind(r.err)), slog.Bool("download_failed", errors.Is(r.err, update.ErrDownload)), slog.Bool("verification_failed", errors.Is(r.err, update.ErrVerification)))
	switch {
	case r.err == nil:
		s.about.Installed = true
		s.about.Restarting = s.config.RestartAfterUpdate
		if s.about.Restarting {
			s.update.exitErr = update.ErrRestart
		}
		s.update.exitAt = time.Now().Add(2 * time.Second)
	case errors.Is(r.err, update.ErrRecovery):
		s.about.Message = messageUpdateRecovery
		s.update.exitAt = time.Now().Add(3 * time.Second)
	default:
		switch {
		case errors.Is(r.err, context.Canceled):
			s.about.Message = messageUpdateCanceled
		case errors.Is(r.err, update.ErrManual):
			s.about.ManualInstall = true
			s.about.Message = messageUpdateManual
		case errors.Is(r.err, syscall.ENOSPC):
			s.about.Message = messageUpdateNoSpace
		case errors.Is(r.err, os.ErrPermission) || errors.Is(r.err, syscall.EROFS):
			s.about.Message = messageUpdateReadOnly
		case isStorageError(r.err):
			s.about.Message = messageUpdateStorageFailed
		case errors.Is(r.err, update.ErrVerification):
			s.about.Message = messageUpdateVerificationFailed
		case errors.Is(r.err, update.ErrDownload):
			s.about.Message = messageUpdateDownloadFailed
		default:
			s.about.Message = messageUpdateInstallFailed
		}
		if s.client != nil {
			s.startRemote()
		}
	}
	return true
}

// close waits for cancellation and any resulting rollback before returning.
func (w *updateWork) close() {
	if w.cancel != nil {
		w.cancel()
	}
	if w.done != nil {
		<-w.done
	}
}

// isStorageError recognizes filesystem failures without exposing local paths.
func isStorageError(err error) bool {
	var path *os.PathError
	var link *os.LinkError
	return errors.As(err, &path) || errors.As(err, &link)
}
