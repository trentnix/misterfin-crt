package playback

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	nativeplayer "misterfin-crt/internal/player/mplayer"
)

func TestDecoderShutdownOwnsProcessGroup(t *testing.T) {
	for _, mode := range []string{"graceful", "forced", "leader-exits"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			script, pidfile := filepath.Join(dir, "decoder"), filepath.Join(dir, "child.pid")
			trap, end := "trap 'exit 0' TERM", "wait"
			if mode != "graceful" {
				trap = "trap '' TERM"
			}
			if mode == "leader-exits" {
				end = "exit 0"
			}
			source := fmt.Sprintf("#!/bin/sh\n%s\nsleep 60 &\necho $! > '%s'\n%s\n", trap, pidfile, end)
			if err := os.WriteFile(script, []byte(source), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p, err := startProcess(ctx, script, nil, &mediaSource{}, nativeplayer.Decoder{})
			if err != nil {
				t.Fatal(err)
			}
			defer syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
			p.feed()
			var child int
			deadline := time.Now().Add(3 * time.Second)
			for child == 0 && time.Now().Before(deadline) {
				data, _ := os.ReadFile(pidfile)
				child, _ = strconv.Atoi(strings.TrimSpace(string(data)))
				if child == 0 {
					time.Sleep(time.Millisecond)
				}
			}
			if child == 0 {
				t.Fatal("decoder descendant did not start")
			}
			if mode != "leader-exits" {
				cancel()
			}
			select {
			case <-p.done:
			case <-time.After(5 * time.Second):
				t.Fatal("decoder shutdown exceeded grace period")
			}
			p.close()
			deadline = time.Now().Add(time.Second)
			for {
				status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", child))
				if os.IsNotExist(err) || strings.Contains(string(status), "State:\tZ") {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if time.Now().After(deadline) {
					t.Fatal("decoder descendant still running after cleanup")
				}
				time.Sleep(time.Millisecond)
			}
		})
	}
}
