package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"misterfin-crt/internal/diagnostics"
	"misterfin-crt/internal/release"
	"misterfin-crt/internal/settings"
	"misterfin-crt/internal/update"
)

// startupDiagnostics owns the process log through display and input cleanup.
// Stages are static labels. Failures never record raw errors or command arguments.
type startupDiagnostics struct {
	log     *diagnostics.Log
	notice  string // Safe startup warning forwarded to the shared UI.
	stage   string
	started time.Time
}

// openStartupDiagnostics starts logging before framebuffer or browser setup.
// The interlaced supervisor has a separate bounded log because its child opens
// the application log independently. Preview runs retain their existing behavior.
func openStartupDiagnostics(o launchOptions, supervisor bool, source *settings.File) (*startupDiagnostics, error) {
	s := &startupDiagnostics{started: time.Now()}
	if !o.browse {
		return s, nil
	}
	server := source.Section("server")
	// Explicit server settings supersede all legacy connection-file switches.
	legacyEnabled := server.Data == nil && server.Err == nil && legacyDebugLog(o.config)
	c, err := diagnostics.ParseConfig(source.Section("diagnostics"), legacyEnabled)
	if err != nil {
		s.notice = "Check diagnostics settings. Logging is off."
		fmt.Fprintln(os.Stderr, s.notice)
		return s, nil
	}
	role := "application"
	if supervisor {
		c.Path += ".supervisor"
		role = "display-supervisor"
	}
	s.log, err = diagnostics.Open(c)
	if err != nil {
		s.notice = "Cannot open diagnostic log. Check its path and permissions. Logging is off."
		fmt.Fprintln(os.Stderr, s.notice)
	}
	s.log.Record("application.start", slog.String("build", release.CurrentBuild().String()),
		slog.String("role", role), slog.Bool("headless", o.headless != ""),
		slog.String("os", runtime.GOOS), slog.String("arch", runtime.GOARCH))
	return s, nil
}

// legacyDebugLog discovers only the switch, even if another configuration line
// is invalid. Reading is bounded and no credential-bearing lines escape here.
func legacyDebugLog(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	scan := bufio.NewScanner(io.LimitReader(f, 1<<20))
	for scan.Scan() {
		if strings.EqualFold(strings.TrimSpace(scan.Text()), "DEBUGLOG") {
			return true
		}
	}
	return false
}

// phase identifies the next startup operation without logging user-supplied data.
func (s *startupDiagnostics) phase(stage string) {
	s.stage = stage
	s.log.Record("application.phase", slog.String("stage", stage), slog.Int64("elapsed_ms", time.Since(s.started).Milliseconds()))
}

// close records the final stage after resources have been released and drains
// accepted events. Diagnostic failures never replace the application's result.
func (s *startupDiagnostics) close(err error) {
	if update.RestartRequested(err) {
		s.log.Record("update.restart")
		err = nil
	}
	if err != nil {
		s.log.Record("application.failure", slog.String("stage", s.stage), slog.String("error_kind", diagnostics.ErrorKind(err)))
	}
	s.log.Record("application.exit", slog.Bool("failed", err != nil), slog.Bool("canceled", errors.Is(err, context.Canceled)), slog.Int64("elapsed_ms", time.Since(s.started).Milliseconds()))
	if s.log.Close() != nil {
		fmt.Fprintln(os.Stderr, "Diagnostics stopped: could not write log.")
	}
}
