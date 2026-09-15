package rendering

import (
	"time"

	"misterfin-crt/internal/release"
)

// AboutPresentation is a value snapshot of the About page and release check.
// The shared renderer receives this state without performing network requests.
type AboutPresentation struct {
	Visible  bool
	Build    release.Build
	Checking bool
	Checked  bool
	Release  release.Status
	Message  string
	// UpdateNoticeUntil keeps the placeholder visible independently of release checks.
	UpdateNoticeUntil time.Time
}

// Status gives the update-action notice priority until its deadline. Background
// checks can finish during that interval without replacing the visible notice.
// The ordinary release status returns on the first frame at or after expiry.
func (a AboutPresentation) Status(now time.Time) string {
	switch {
	case now.Before(a.UpdateNoticeUntil):
		return "Not implemented yet."
	case a.Checking:
		return "Checking for updates..."
	case a.Message != "":
		return a.Message
	case a.Release.Available:
		return "Release " + a.Release.Latest + " available"
	case a.Checked:
		return "Up to date"
	default:
		return "Update checks unavailable"
	}
}
