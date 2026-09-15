//go:build !linux || !cgo

package displaymode

import "errors"

func enableConsole() error { return errors.New("interlaced MiSTer output requires Linux with cgo") }

func restoreConsole() error { return nil }
