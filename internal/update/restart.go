package update

import "errors"

// ErrRestart requests a new process after a successful installation. Callers
// must finish cleanup before acting on it. Any additional error prevents restart.
var ErrRestart = errors.New("restart after update")

// RestartExitCode tells the MiSTer launcher to start the installed launcher again.
// The interlaced supervisor forwards it only after restoring the normal core.
const RestartExitCode = 75

// RestartRequested reports whether err contains only a restart request, including
// wrappers added by deferred cleanup. A joined cleanup failure must remain fatal.
func RestartRequested(err error) bool {
	if err == ErrRestart {
		return true
	}
	switch e := err.(type) {
	case interface{ Unwrap() []error }:
		children := e.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !RestartRequested(child) {
				return false
			}
		}
		return true
	case interface{ Unwrap() error }:
		return RestartRequested(e.Unwrap())
	default:
		return false
	}
}
