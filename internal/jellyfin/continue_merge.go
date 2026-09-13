package jellyfin

import (
	"sort"
	"time"
)

// mergeContinue favors the most recently resumed episode for each series,
// regardless of the next-up rank. Unknown series dates retain server order
// after entries whose playback activity is known.
func mergeContinue(resume, next, history []Item) []Item {
	activity := make(map[string]time.Time)
	for _, item := range append(append([]Item(nil), history...), resume...) {
		if item.SeriesID != "" && lastPlayed(item).After(activity[item.SeriesID]) {
			activity[item.SeriesID] = lastPlayed(item)
		}
	}
	resume = append([]Item(nil), resume...)
	sort.SliceStable(resume, func(i, j int) bool { return lastPlayed(resume[i]).After(lastPlayed(resume[j])) })
	seenItems, seenSeries := make(map[string]bool), make(map[string]bool)
	result := make([]Item, 0, len(resume)+len(next))
	add := func(item Item, action string) {
		if item.ID == "" || seenItems[item.ID] || (item.SeriesID != "" && seenSeries[item.SeriesID]) {
			return
		}
		item.ContinueAction = action
		seenItems[item.ID] = true
		if item.SeriesID != "" {
			seenSeries[item.SeriesID] = true
		}
		result = append(result, item)
	}
	for _, item := range resume {
		if !item.UserData.Played && item.UserData.PlaybackPositionTicks > 0 {
			add(item, "resume")
		}
	}
	for _, item := range next {
		item.Type = "Episode"
		if !item.UserData.Played && item.UserData.PlaybackPositionTicks == 0 {
			add(item, "next")
		}
	}
	rank := func(item Item) time.Time {
		when := lastPlayed(item)
		if item.SeriesID != "" && activity[item.SeriesID].After(when) {
			when = activity[item.SeriesID]
		}
		return when
	}
	sort.SliceStable(result, func(i, j int) bool { return rank(result[i]).After(rank(result[j])) })
	return result
}
