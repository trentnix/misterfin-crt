package rendering

import (
	"mistervision/internal/jellyfin"
	"testing"
)

func TestContinueLabels(t *testing.T) {
	season, episode := 1, 4
	item := jellyfin.Item{Name: "Episode name", Type: "Episode", SeriesName: "Dungeons and Dragons", ParentIndexNumber: &season, IndexNumber: &episode, ContinueAction: "next"}
	if got := continueSubtitle(item); got != "Next · S1 E4" {
		t.Fatal(got)
	}
	if got := itemTitle(item); got != "Dungeons and Dragons - Episode name" {
		t.Fatal(got)
	}
	item.ContinueAction = "resume"
	item.UserData.PlaybackPositionTicks = 7540000000
	if got := continueSubtitle(item); got != "Resume · 12:34 · S1 E4" {
		t.Fatal(got)
	}
	item.ParentIndexNumber = nil
	item.IndexNumber = nil
	if got := continueSubtitle(item); got != "Resume · 12:34" {
		t.Fatal("unknown numbering invented", got)
	}
}
