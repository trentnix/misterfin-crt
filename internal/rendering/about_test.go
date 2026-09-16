package rendering

import (
	"strings"
	"testing"

	"misterfin-crt/internal/input/control"
)

func TestReleaseNotesWrapAndRemoveDecoration(t *testing.T) {
	lines := ReleaseNotes("## Changes\n[Read this](https://example.com) **first**.\n"+strings.Repeat("x", 80)+"\nBad\x1bcontrol", 320)
	text := strings.Join(lines, "\n")
	for _, unwanted := range []string{"https://", "**", "##", "\x1b"} {
		if strings.Contains(text, unwanted) {
			t.Fatal(text)
		}
	}
	for _, line := range lines {
		if len([]rune(line)) > (320-48)/8 {
			t.Fatalf("line exceeds width: %q", line)
		}
	}
	if !strings.Contains(text, "Read this first.") {
		t.Fatal(text)
	}
}

func TestReleaseNoteScrollGeometryMatchesFooter(t *testing.T) {
	a := AboutPresentation{CanInstall: true, Notes: make([]string, 100)}
	for _, height := range []int{240, 288, 480} {
		labels := control.Labels{control.Back: "An unusually long controller button", control.Up: "Up", control.Down: "Down"}
		layout := a.notesLayout(640, height, labels)
		if a.ScrollLimit(640, height, labels) != 100-layout.rows {
			t.Fatal("input and rendering scroll limits differ")
		}
		if layout.top+layout.rows*12 > layout.statusY {
			t.Fatal("notes overlap status")
		}
	}
}

func TestInstallationCompletionStatus(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		a := AboutPresentation{Installed: true, Restarting: automatic}
		want := "Installed. Reopen MiSTerFin CRT."
		if automatic {
			want = "Update installed. Restarting..."
		}
		if got := a.Status(); got != want {
			t.Fatalf("completion status: %q, want %q", got, want)
		}
	}
}
