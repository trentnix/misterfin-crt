package displaymode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"misterfin-crt/internal/update"
)

// ActiveEnv is inherited only by the supervised client and its native player.
// It enables the shared scanout protocol after Main has stopped using SPI.
const ActiveEnv = "MISTERFIN_CRT_INTERLACED"

// LauncherEnv delegates successful menu restoration to the Scripts launcher.
// Failures and standalone invocations always restore the menu in the supervisor.
const LauncherEnv = "MISTERFIN_CRT_LAUNCHER"

// Run starts a supervised copy of the client under the standalone interlaced
// core. It owns the core switch, exclusive hardware access, and failure recovery.
// If LauncherEnv is "1", the launcher must restore the menu after successful exit.
// An update restart restores the normal core before returning update.ErrRestart.
// Arguments are the original client arguments without the executable name.
func Run(ctx context.Context, directory string, args []string) (err error) {
	directory, err = filepath.Abs(directory)
	if err != nil {
		return err
	}
	lock, err := os.OpenFile("/tmp/misterfin-crt-display.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("another interlaced client owns the display")
	}
	active, err := os.ReadFile("/tmp/CORENAME")
	if err != nil || strings.TrimSpace(string(active)) != "MENU" {
		return errors.New("start interlaced playback from the normal MiSTer menu")
	}
	mgl, err := prepareCore(directory)
	if err != nil {
		return err
	}
	owner := strconv.Itoa(os.Getpid())
	var mainPID int
	defer func() {
		err = errors.Join(err, stopOrphans(owner), restoreConsole())
		if mainPID > 0 {
			err = errors.Join(err, syscall.Kill(mainPID, syscall.SIGCONT))
		}
		if err == nil && os.Getenv(LauncherEnv) == "1" {
			// Main can now accept the launcher's one normal menu-return command.
			return
		}
		// Restore even if the load command timed out: Main may have received it.
		restore, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err = errors.Join(err, command(restore, "load_core /media/fat/menu.rbf"))
		err = errors.Join(err, waitCore(restore, "MENU", false))
	}()
	if err = prepareConsole(); err != nil {
		return err
	}
	// Main publishes OSD_VISIBLE when its menu can receive F9. The framebuffer
	// geometry appears earlier, while F9 is still routed to the core. Discard
	// any marker from a previous run before asking Main to load this core.
	const menuReady = "/tmp/OSD_VISIBLE"
	if err = os.Remove(menuReady); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = command(ctx, "load_core "+mgl); err != nil {
		return err
	}
	ready, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if err = waitCore(ready, coreName, true); err != nil {
		return err
	}
	if err = waitMenuReady(ready, menuReady); err != nil {
		return err
	}
	if err = enableConsole(); err != nil {
		return err
	}
	mainPID, err = stopMain(ready)
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	child := exec.CommandContext(ctx, executable, args...)
	child.Env = append(os.Environ(), ActiveEnv+"=1", ownerEnv+"="+owner)
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	child.Cancel = func() error { return child.Process.Signal(syscall.SIGTERM) }
	child.WaitDelay = 5 * time.Second
	return childResult(child.Run())
}

// childResult preserves an update restart through the supervisor. Deferred
// hardware cleanup joins its errors, preventing a restart if restoration fails.
func childResult(err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == update.RestartExitCode {
		return update.ErrRestart
	}
	return err
}

// command bounds FIFO access, including when Main is stopped or restarting.
func command(ctx context.Context, value string) error {
	deadline, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for {
		fd, err := syscall.Open("/dev/MiSTer_cmd", syscall.O_WRONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
		if err == nil {
			n, e := syscall.Write(fd, []byte(value+"\n"))
			syscall.Close(fd)
			if e == nil && n == len(value)+1 {
				return nil
			}
			if e != syscall.EAGAIN {
				if e == nil {
					e = errors.New("short command write")
				}
				return fmt.Errorf("send MiSTer command: %w", e)
			}
		} else if err != syscall.ENXIO && err != syscall.ENOENT {
			return err
		}
		if err := delay(deadline); err != nil {
			return fmt.Errorf("MiSTer command timed out: %w", err)
		}
	}
}

// waitCore requires both the MGL identity and a full-height framebuffer.
func waitCore(ctx context.Context, name string, full bool) error {
	for {
		current, _ := os.ReadFile("/tmp/CORENAME")
		mode, _ := os.ReadFile("/sys/module/MiSTer_fb/parameters/mode")
		var format, swap, width, height, stride int
		n, _ := fmt.Sscanf(string(mode), "%d %d %d %d %d", &format, &swap, &width, &height, &stride)
		if strings.TrimSpace(string(current)) == name && (!full || n == 5 && width == 640 && (height == 480 || height == 576) && stride >= width*4) {
			return nil
		}
		if err := delay(ctx); err != nil {
			return fmt.Errorf("waiting for %s display: %w", name, err)
		}
	}
}

// delay yields between bounded hardware-state checks without busy polling.
func delay(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(50 * time.Millisecond):
		return nil
	}
}

// waitMenuReady waits for Main's log_file_entry marker. A ready framebuffer does
// not imply that Main routes F9 to its menu yet. The caller removes stale state
// before loading the core and must provide a bounded context.
func waitMenuReady(ctx context.Context, path string) error {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err == nil && strings.TrimSpace(string(data)) == "1" {
			return nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for MiSTer menu input: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
