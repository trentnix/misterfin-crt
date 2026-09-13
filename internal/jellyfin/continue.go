package jellyfin

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// ContinueWatching combines the server's resumable videos and next episodes.
// Both lists are fetched independently. A partial result carries an error so
// callers can keep usable entries visible and offer retry. Recent episode
// history supplies cross-list recency without one request per series.
func (c *Client) ContinueWatching(ctx context.Context) (Page, error) {
	var resume, next, history []Item
	var resumeErr, nextErr error
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		resume, resumeErr = c.continueItems(ctx, "/UserItems/Resume", url.Values{"MediaTypes": {"Video"}})
	}()
	go func() {
		defer wg.Done()
		next, nextErr = c.continueItems(ctx, "/Shows/NextUp", url.Values{"enableResumable": {"false"}})
	}()
	go func() {
		defer wg.Done()
		q := homeQuery(c.Session.UserID)
		q.Set("Recursive", "true")
		q.Set("IncludeItemTypes", "Episode")
		q.Set("IsPlayed", "true")
		q.Set("SortBy", "DatePlayed")
		q.Set("SortOrder", "Descending")
		q.Set("Limit", "256")
		var p Page
		if c.json(ctx, "GET", "/Items", q, nil, &p) == nil {
			history = p.Items
		}
	}()
	wg.Wait()
	if ctx.Err() != nil {
		return Page{}, ctx.Err()
	}
	items := mergeContinue(resume, next, history)
	total := len(items)
	return Page{Items: items, TotalRecordCount: &total}, errors.Join(resumeErr, nextErr)
}

func homeQuery(user string) url.Values {
	return url.Values{"userId": {user}, "Fields": {"ProductionYear,RunTimeTicks,SeriesInfo"}, "EnableUserData": {"true"}, "ImageTypeLimit": {"1"}, "EnableImageTypes": {"Primary,Backdrop"}, "EnableTotalRecordCount": {"true"}}
}

// continueItems reads complete source lists before deduplicating them. Paging
// each source after merging would otherwise create duplicates or skip series.
func (c *Client) continueItems(ctx context.Context, path string, extra url.Values) ([]Item, error) {
	q := homeQuery(c.Session.UserID)
	for key, values := range extra {
		q[key] = values
	}
	const batch = 128
	q.Set("Limit", strconv.Itoa(batch))
	var items []Item
	seen := make(map[string]bool)
	for start := 0; ; {
		q.Set("StartIndex", strconv.Itoa(start))
		var p Page
		if err := c.json(ctx, "GET", path, q, nil, &p); err != nil {
			return items, err
		}
		if p.Items == nil || (p.TotalRecordCount != nil && *p.TotalRecordCount < 0) {
			return items, errors.New("invalid Continue Watching response")
		}
		added := 0
		for _, item := range p.Items {
			if item.ID == "" {
				return items, errors.New("Continue Watching item is missing its ID")
			}
			if !seen[item.ID] {
				seen[item.ID] = true
				items = append(items, item)
				added++
			}
		}
		start += len(p.Items)
		if len(p.Items) == 0 && p.TotalRecordCount != nil && start < *p.TotalRecordCount {
			return items, errors.New("Continue Watching pagination ended early")
		}
		if len(p.Items) == 0 || (p.TotalRecordCount != nil && start >= *p.TotalRecordCount) || (p.TotalRecordCount == nil && len(p.Items) < batch) {
			return items, nil
		}
		if added == 0 {
			return items, errors.New("Continue Watching pagination did not advance")
		}
		if len(items) >= 10000 {
			return items, errors.New("Continue Watching exceeds 10000 source items")
		}
	}
}

// lastPlayed tolerates omitted dates. Next-up episodes usually have no played
// date themselves, so their series' recent history supplies it when available.
func lastPlayed(item Item) time.Time {
	if item.UserData.LastPlayedDate != nil {
		return *item.UserData.LastPlayedDate
	}
	return time.Time{}
}
