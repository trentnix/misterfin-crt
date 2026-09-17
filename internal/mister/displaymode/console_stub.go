//go:build !linux || !cgo

package displaymode

import "errors"

func prepareConsole() error { return errors.New("interlaced MiSTer output requires Linux with cgo") }

func enableConsole() error {
	return errors.New("interlaced MiSTer output requires Linux with cgo")
}

func restoreConsole() error { return nil }
