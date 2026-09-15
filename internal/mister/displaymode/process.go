package displaymode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const ownerEnv = "MISTERFIN_CRT_DISPLAY_OWNER"

// ownedProcesses finds only descendants carrying this supervisor's marker.
// The marker survives reparenting after a client crash, unlike PPid. Environments
// stay private and are never logged. Main does not inherit the marker.
func ownedProcesses(root, owner string) []int {
	entries, _ := os.ReadDir(root)
	marker := []byte(ownerEnv + "=" + owner)
	var pids []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, entry.Name(), "environ"))
		if err != nil {
			continue
		}
		for _, variable := range bytes.Split(data, []byte{0}) {
			if bytes.Equal(variable, marker) {
				pids = append(pids, pid)
				break
			}
		}
	}
	return pids
}

// stopOrphans runs after the client exits and before Main resumes SPI traffic.
// Normally no descendants remain. After a crash, decoder/cache processes must
// stop before a core reload invalidates their framebuffer and FPGA mappings.
func stopOrphans(owner string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		pids := ownedProcesses("/proc", owner)
		if len(pids) == 0 {
			return nil
		}
		for _, pid := range pids {
			if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				return err
			}
		}
		if err := delay(ctx); err != nil {
			return err
		}
	}
}

// stopMain waits for Main to settle after a core load, then pauses it before
// direct SPI access. A nonzero PID must be resumed, even when confirmation fails.
func stopMain(ctx context.Context) (int, error) {
	var pid int
	for {
		var err error
		pid, err = findMain()
		if err == nil {
			break
		}
		if e := delay(ctx); e != nil {
			return 0, errors.Join(err, e)
		}
	}
	if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
		return 0, err
	}
	return pid, waitStopped(ctx, pid)
}

// findMain refuses ambiguous ownership rather than stopping an unrelated process.
func findMain() (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}
	pid := 0
	for _, entry := range entries {
		n, e := strconv.Atoi(entry.Name())
		if e != nil {
			continue
		}
		name, _ := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
		if strings.TrimSpace(string(name)) != "MiSTer" {
			continue
		}
		status, _ := os.ReadFile(filepath.Join("/proc", entry.Name(), "status"))
		if strings.Contains(string(status), "State:\tZ") || strings.Contains(string(status), "State:\tX") {
			continue
		}
		if pid != 0 {
			return 0, errors.New("multiple MiSTer processes found")
		}
		pid = n
	}
	if pid == 0 {
		return 0, errors.New("MiSTer process not found")
	}
	return pid, nil
}

// waitStopped confirms exclusive SPI ownership after sending SIGSTOP.
func waitStopped(ctx context.Context, pid int) error {
	for {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "State:") && strings.Contains(line, "T") {
				return nil
			}
		}
		if err := delay(ctx); err != nil {
			return err
		}
	}
}
