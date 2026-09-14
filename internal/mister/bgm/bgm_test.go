package bgm

import (
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSuspendRestoresOnlyEnabledPlaylists(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		want         []string
	}{
		{"random", "yes\trandom\tMusic\ttrack.mp3\n", []string{"status", "stop", "play"}},
		{"loop", "yes\tloop\tMusic\ttrack.mp3\n", []string{"status", "stop", "play"}},
		{"between tracks", "no\trandom\tMusic\t\n", []string{"status", "stop", "play"}},
		{"disabled", "no\tdisabled\tMusic\t\n", []string{"status"}},
		{"disabled while stopping", "yes\tdisabled\tMusic\ttrack.mp3\n", []string{"status"}},
		{"unknown mode", "yes\tunknown\tMusic\ttrack.mp3\n", []string{"status"}},
		{"missing mode", "yes\t\tMusic\ttrack.mp3\n", []string{"status"}},
		{"truncated", "yes\tran", []string{"status"}},
		{"malformed", "unavailable", []string{"status"}},
		{"oversized", strings.Repeat("x", 256) + "\trandom\t", []string{"status"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, commands := serveBGM(t, tc.status)
			restore := suspend(path, time.Second)
			// Restoring twice must not restart the playlist twice.
			restore()
			restore()
			for _, want := range tc.want {
				select {
				case got := <-commands:
					if got != want {
						t.Fatalf("command = %q, want %q", got, want)
					}
				case <-time.After(time.Second):
					t.Fatalf("missing command %q", want)
				}
			}
			select {
			case got := <-commands:
				t.Fatalf("unexpected command %q", got)
			case <-time.After(20 * time.Millisecond):
			}
		})
	}
}

func TestMissingBGMIsHarmless(t *testing.T) {
	restore := suspend(filepath.Join(t.TempDir(), "missing.sock"), time.Second)
	restore()
	restore()
}

func TestUnresponsiveBGMDoesNotBlockStartup(t *testing.T) {
	listener, path := listenBGM(t)
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	start := time.Now()
	restore := suspend(path, 30*time.Millisecond)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("unresponsive BGM blocked startup for %s", elapsed)
	}
	restore()
	select {
	case conn := <-accepted:
		conn.Close()
	case <-time.After(time.Second):
		t.Fatal("status request never connected")
	}
}

func TestFailedStopDoesNotRestore(t *testing.T) {
	listener, path := listenBGM(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		buf := make([]byte, len("status"))
		if _, err := io.ReadFull(conn, buf); err != nil {
			return
		}
		// Remove the listener before returning status, making stop fail to dial.
		listener.Close()
		_, _ = io.WriteString(conn, "yes\trandom\tMusic\ttrack.mp3\n")
	}()
	restore := suspend(path, time.Second)
	<-done
	// A restarted service must not receive play when stop was never delivered.
	restarted, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	restore()
	if err := restarted.SetDeadline(time.Now().Add(30 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	conn, err := restarted.Accept()
	if err == nil {
		conn.Close()
		t.Fatal("restored BGM after failed stop")
	}
	if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
		t.Fatal(err)
	}
}

// listenBGM opens a private socket for protocol tests without touching menu music.
func listenBGM(t *testing.T) (*net.UnixListener, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bgm.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	return listener, path
}

// serveBGM records raw commands and fragments status replies. No newline is
// required on requests, matching the C client's one-connection-per-command protocol.
func serveBGM(t *testing.T, status string) (string, <-chan string) {
	t.Helper()
	listener, path := listenBGM(t)
	commands := make(chan string, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.SetDeadline(time.Now().Add(time.Second))
			var buf [32]byte
			n, _ := conn.Read(buf[:])
			name := string(buf[:n])
			commands <- name
			if name == "status" {
				for _, part := range strings.SplitAfter(status, "\t") {
					if _, err := io.WriteString(conn, part); err != nil {
						break
					}
				}
			}
			conn.Close()
		}
	}()
	t.Cleanup(func() { listener.Close(); <-done })
	return path, commands
}
