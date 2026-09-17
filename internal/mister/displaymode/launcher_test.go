package displaymode

import (
	"context"
	"errors"
	"mistervision/internal/update"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestLauncherMenuReturn exercises the installed script against a private FIFO
// and a client stub. The stub models the supervisor's launcher contract without
// opening the host's consoles or switching a real core.
func TestLauncherMenuReturn(t *testing.T) {
	source, err := os.ReadFile("../../../tools/mistervision.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"240p", "480i", "480i failure"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			fifo := filepath.Join(dir, "commands")
			if err := syscall.Mkfifo(fifo, 0600); err != nil {
				t.Fatal(err)
			}
			fd, err := syscall.Open(fifo, syscall.O_RDWR|syscall.O_NONBLOCK, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer syscall.Close(fd)
			fat := filepath.Join(dir, "fat")
			app := filepath.Join(fat, "mistervision")
			if err := os.MkdirAll(app, 0700); err != nil {
				t.Fatal(err)
			}
			helper := `#!/bin/bash
set -eu
if [ "$TEST_MODE" = "480i failure" ]; then
 printf '%s\n' "load_core $TEST_MENU" > "$TEST_FIFO"
 echo 'client failed' >&2
 exit 1
fi
if [ "$TEST_MODE" = "480i" ] && [ "${MISTERVISION_LAUNCHER:-}" != "1" ]; then
 printf '%s\n' "load_core $TEST_MENU" > "$TEST_FIFO"
fi
`
			for name, data := range map[string]string{
				filepath.Join(app, "mistervision"): helper,
				filepath.Join(app, "mplayer-arm"):  "#!/bin/sh\nexit 0\n",
				filepath.Join(dir, "taskset"):      "#!/bin/sh\nexit 0\n",
				filepath.Join(fat, "menu.rbf"):     "test core",
			} {
				if err := os.WriteFile(name, []byte(data), 0700); err != nil {
					t.Fatal(err)
				}
			}
			script := strings.NewReplacer("/dev/tty0", filepath.Join(dir, "console"), "/dev/MiSTer_cmd", fifo, "/media/fat", fat).Replace(string(source))
			path := filepath.Join(dir, "launch.sh")
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", path)
			cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "TEST_MODE="+mode, "TEST_FIFO="+fifo, "TEST_MENU="+filepath.Join(fat, "menu.rbf"))
			output, err := cmd.CombinedOutput()
			if mode == "480i failure" {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(output), "client failed") {
					t.Fatalf("lost failure: %v %s", err, output)
				}
			} else if err != nil {
				t.Fatalf("launcher: %v %s", err, output)
			}
			buffer := make([]byte, 4096)
			n, err := syscall.Read(fd, buffer)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := string(buffer[:n]), "load_core "+filepath.Join(fat, "menu.rbf")+"\n"; got != want {
				t.Fatalf("menu return: got %q, want %q", got, want)
			}
		})
	}
}

// TestLauncherUpdateRestart simulates atomic replacement while the old script
// is running from a temporary copy. Only the installed launcher may restart it.
func TestLauncherUpdateRestart(t *testing.T) {
	source, err := os.ReadFile("../../../tools/mistervision.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		interlaced bool
		nextStatus int
		missing    bool
	}{
		{"240p", false, 0, false},
		{"480i", true, 0, false},
		{"new client fails", false, 1, false},
		{"launcher missing", false, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			fat := filepath.Join(dir, "fat")
			app := filepath.Join(fat, "mistervision")
			scripts := filepath.Join(fat, "Scripts")
			for _, path := range []string{app, scripts} {
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			fifo := filepath.Join(dir, "commands")
			if err := syscall.Mkfifo(fifo, 0600); err != nil {
				t.Fatal(err)
			}
			fd, err := syscall.Open(fifo, syscall.O_RDWR|syscall.O_NONBLOCK, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer syscall.Close(fd)
			script := strings.NewReplacer("/dev/tty0", filepath.Join(dir, "console"), "/dev/MiSTer_cmd", fifo, "/media/fat", fat).Replace(string(source))
			// The replacement records entry before running its own startup.
			replacement := "#!/bin/bash\nprintf 'updated launcher\\n' >> \"$TEST_EVENTS\"\n" + script
			helper := `#!/bin/bash
set -eu
[ "$MISTERVISION_AUTO_RESTART" = 1 ]
if [ ! -f "$TEST_STARTED" ]; then
    touch "$TEST_STARTED"
    printf 'old client\n' >> "$TEST_EVENTS"
    if [ "$TEST_MISSING" != true ]; then
        cp "$TEST_REPLACEMENT" "$TEST_LAUNCHER.new"
        chmod +x "$TEST_LAUNCHER.new"
        mv "$TEST_LAUNCHER.new" "$TEST_LAUNCHER"
    fi
    if [ "$TEST_INTERLACED" = true ]; then
        printf 'restored core\n' >> "$TEST_EVENTS"
        printf 'load_core %s\n' "$TEST_MENU" > "$TEST_FIFO"
    fi
    exit "$TEST_RESTART_STATUS"
fi
printf 'new client\n' >> "$TEST_EVENTS"
exit "$TEST_NEXT_STATUS"
`
			files := map[string]string{
				filepath.Join(dir, "launch-copy.sh"): script,
				filepath.Join(dir, "replacement.sh"): replacement,
				filepath.Join(dir, "taskset"):        "#!/bin/sh\nexit 0\n",
				filepath.Join(fat, "menu.rbf"):       "test core",
				filepath.Join(app, "mistervision"):   helper,
				filepath.Join(app, "mplayer-arm"):    "#!/bin/sh\nexit 0\n",
			}
			for path, data := range files {
				if err := os.WriteFile(path, []byte(data), 0700); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "bash", filepath.Join(dir, "launch-copy.sh"))
			cmd.Env = append(os.Environ(),
				"PATH="+dir+":"+os.Getenv("PATH"),
				"TEST_EVENTS="+filepath.Join(dir, "events"),
				"TEST_STARTED="+filepath.Join(dir, "started"),
				"TEST_REPLACEMENT="+filepath.Join(dir, "replacement.sh"),
				"TEST_LAUNCHER="+filepath.Join(scripts, "MiSTerVision.sh"),
				"TEST_MENU="+filepath.Join(fat, "menu.rbf"), "TEST_FIFO="+fifo,
				"TEST_INTERLACED="+strconv.FormatBool(tc.interlaced),
				"TEST_MISSING="+strconv.FormatBool(tc.missing),
				"TEST_RESTART_STATUS="+strconv.Itoa(update.RestartExitCode),
				"TEST_NEXT_STATUS="+strconv.Itoa(tc.nextStatus))
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("restart loop: %v", ctx.Err())
			}
			if (tc.nextStatus != 0 || tc.missing) != (err != nil) {
				t.Fatalf("unexpected exit: %v %s", err, out)
			}
			events, err := os.ReadFile(filepath.Join(dir, "events"))
			if err != nil {
				t.Fatal(err)
			}
			want := "old client\n"
			if tc.interlaced {
				want += "restored core\n"
			}
			if !tc.missing {
				want += "updated launcher\nnew client\n"
			}
			if string(events) != want {
				t.Fatalf("handoff: %q, want %q", events, want)
			}
			buffer := make([]byte, 4096)
			n, err := syscall.Read(fd, buffer)
			if err == syscall.EAGAIN {
				n = 0
			} else if err != nil {
				t.Fatal(err)
			}
			returns := 0
			if tc.interlaced {
				returns++
			}
			if tc.nextStatus == 0 && !tc.missing {
				returns++
			}
			want = strings.Repeat("load_core "+filepath.Join(fat, "menu.rbf")+"\n", returns)
			if string(buffer[:n]) != want {
				t.Fatalf("menu restoration: %q, want %q", buffer[:n], want)
			}
		})
	}
}
