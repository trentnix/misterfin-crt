package rendering

import (
	"fmt"
	"regexp"
	"strings"

	"misterfin-crt/internal/release"
	"misterfin-crt/internal/update"
)

// AboutPresentation is a value snapshot of the About page and release check.
// Notes is immutable after publication. Rendering performs no installation I/O.
type AboutPresentation struct {
	Visible                         bool
	Build                           release.Build
	Checking, Checked               bool
	Release                         release.Status
	Message                         string
	NotesVisible                    bool
	Notes                           []string
	Scroll                          int
	CanInstall, Updating, Installed bool
	// Restarting distinguishes a supported automatic restart from manual relaunch.
	Restarting bool
	Progress   update.Progress
}

// Status returns safe user-facing release or installation state.
func (a AboutPresentation) Status() string {
	switch {
	case a.Installed && a.Restarting:
		return "Update installed. Restarting..."
	case a.Installed:
		return "Installed. Reopen MiSTerFin CRT."
	case a.Updating:
		switch a.Progress.Phase {
		case update.Validating:
			return "Verifying update and preparing backup..."
		case update.Installing:
			return "Installing update..."
		default:
			if a.Progress.Total > 0 {
				return fmt.Sprintf("Downloading update... %d%%", min(100, a.Progress.Received*100/a.Progress.Total))
			}
			return "Downloading update..."
		}
	case a.Checking:
		return "Checking for updates..."
	case a.Message != "":
		return a.Message
	case a.NotesVisible && !a.CanInstall:
		return "Install the release ZIP manually on this device."
	case a.NotesVisible && !a.Release.HasBundle:
		return "No installation bundle is available."
	case a.NotesVisible:
		return "Install this release? Settings and sign-in are kept."
	case a.Release.Available:
		return "Release " + a.Release.Latest + " available"
	case a.Checked:
		return "Up to date"
	default:
		return "Update checks unavailable"
	}
}

var releaseLink = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)

// ReleaseNotes prepares bounded plain-text lines once when metadata arrives.
// It strips common Markdown decoration and wraps long words within CRT margins.
func ReleaseNotes(text string, width int) []string {
	text = releaseLink.ReplaceAllString(text, "$1")
	text = strings.Map(func(r rune) rune {
		if r == '\r' || (r < 32 && r != '\n' && r != '\t') || r == 127 {
			return -1
		}
		return r
	}, text)
	if strings.TrimSpace(text) == "" {
		text = "No release notes were provided."
	}
	columns := max(8, (width-48)/8)
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		paragraph = strings.TrimLeft(strings.TrimSpace(paragraph), "# ")
		paragraph = strings.ReplaceAll(strings.ReplaceAll(paragraph, "`", ""), "**", "")
		line := ""
		for _, word := range strings.Fields(paragraph) {
			if line != "" && len([]rune(line+" "+word)) > columns {
				lines = append(lines, line)
				line = ""
			}
			runes := []rune(word)
			for len(runes) > columns {
				lines = append(lines, string(runes[:columns]))
				runes = runes[columns:]
			}
			if line != "" {
				line += " "
			}
			line += string(runes)
		}
		lines = append(lines, line)
	}
	return lines
}
