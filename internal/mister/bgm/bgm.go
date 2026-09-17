// Package bgm coordinates MiSTer's optional menu music through its control socket.
// MiSTerVision runs under the Menu core, so BGM's core-change detection cannot stop
// menu music for it. The protocol follows the C client's BGM integration.
package bgm

import (
	"bufio"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	socketPath     = "/tmp/bgm.sock"
	commandTimeout = 250 * time.Millisecond
)

// Suspend stops an enabled BGM playlist and returns a function that restores it.
// The caller must defer restoration until application playback has stopped.
// Disabled, missing, or unresponsive BGM is left alone. Each command has a
// bounded wait, and restoration remains available after application cancellation.
// The returned function is always safe to call and restores at most once.
// Restoration sends play. It does not restore an exact track position.
func Suspend() func() { return suspend(socketPath, commandTimeout) }

// suspend checks the configured playback mode instead of the transient playing
// flag, which can be false between tracks. Failed stop delivery must not cause
// an unsolicited play command when the application exits.
func suspend(path string, timeout time.Duration) func() {
	mode, err := command(path, "status", timeout)
	if err != nil || (mode != "random" && mode != "loop") {
		return func() {}
	}
	if _, err := command(path, "stop", timeout); err != nil {
		return func() {}
	}
	return sync.OnceFunc(func() { _, _ = command(path, "play", timeout) })
}

// command opens one connection per request, as the C client does. Status returns
// the playback field from "playing\tplayback\tplaylist\tfile". Reading only the
// first two fields avoids depending on file length or a connection-close reply.
// Stop and play confirm delivery only. The protocol supplies no acknowledgment.
func command(path, name string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	conn, err := net.DialTimeout("unix", path, timeout)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if err := conn.SetDeadline(deadline); err != nil {
		return "", err
	}
	if _, err := io.WriteString(conn, name); err != nil {
		return "", err
	}
	if name != "status" {
		return "", nil
	}
	reader := bufio.NewReader(io.LimitReader(conn, 256))
	if _, err := reader.ReadString('\t'); err != nil {
		return "", err
	}
	mode, err := reader.ReadString('\t')
	return strings.TrimSuffix(mode, "\t"), err
}
