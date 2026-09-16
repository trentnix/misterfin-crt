package displaymode

import (
	"errors"
	"os/exec"
	"strconv"
	"testing"

	"misterfin-crt/internal/update"
)

func TestChildRestartStatus(t *testing.T) {
	for _, status := range []int{0, 1, update.RestartExitCode} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			err := exec.Command("sh", "-c", "exit "+strconv.Itoa(status)).Run()
			got := childResult(err)
			if status == update.RestartExitCode {
				if !update.RestartRequested(got) {
					t.Fatalf("restart status lost: %v", got)
				}
				if update.RestartRequested(errors.Join(got, errors.New("core restoration failed"))) {
					t.Fatal("restart permitted after failed core restoration")
				}
			} else if got != err {
				t.Fatalf("ordinary exit changed: %v -> %v", err, got)
			}
		})
	}
}
