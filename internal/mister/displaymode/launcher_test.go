package displaymode

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// TestLauncherMenuReturn exercises the installed script against a private FIFO
// and a client stub. The stub models the supervisor's launcher contract without
// opening the host's consoles or switching a real core.
func TestLauncherMenuReturn(t *testing.T) {
	source, err := os.ReadFile("../../../tools/misterfin-crt.sh")
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
			app := filepath.Join(fat, "misterfin-crt")
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
if [ "$TEST_MODE" = "480i" ] && [ "${MISTERFIN_CRT_LAUNCHER:-}" != "1" ]; then
 printf '%s\n' "load_core $TEST_MENU" > "$TEST_FIFO"
fi
`
			for name, data := range map[string]string{
				filepath.Join(app, "misterfin-crt"): helper,
				filepath.Join(app, "mplayer-arm"):   "#!/bin/sh\nexit 0\n",
				filepath.Join(dir, "taskset"):       "#!/bin/sh\nexit 0\n",
				filepath.Join(fat, "menu.rbf"):      "test core",
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
