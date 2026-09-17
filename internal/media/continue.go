package media

import (
	"sort"
	"time"
)

// MergeContinueWatching combines resumable videos with next-episode candidates.
// Each series appears once, favoring its most recently played resumable episode.
// History supplies series activity dates. Undated entries retain provider order
// after dated entries. Adapters must normalize next to episode items first.
// Input slices and items are not modified. Returned item values retain references
// to input metadata, which callers must treat as immutable.
func MergeContinueWatching(resume, next, history []Item) []Item {
	activity := make(map[string]time.Time)
	for _, items := range [][]Item{history, resume} {
		for _, item := range items {
			if item.SeriesID != "" && lastPlayed(item).After(activity[item.SeriesID]) {
				activity[item.SeriesID] = lastPlayed(item)
			}
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

// lastPlayed tolerates omitted dates. Next-up episodes usually have no played
// date themselves, so their series' recent history supplies it when available.
func lastPlayed(item Item) time.Time {
	if item.UserData.LastPlayedDate != nil {
		return *item.UserData.LastPlayedDate
	}
	return time.Time{}
}
