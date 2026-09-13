// Package browser coordinates Jellyfin browsing, media controls, and shared UI
// rendering. Run owns the event loop. Model tracks navigation, PlaybackController
// tracks decoder transitions, and Renderer produces frames for videoout.Output.
// Output implementations select physical presentation without changing UI rules.
package browser

import (
	"fmt"
	"time"

	"misterfin-go/internal/jellyfin"
)

const PageSize = 64

// View retains one navigation screen. Start is the absolute index of the first
// retained row. Selected and Scroll index that window. Target and PendingStart
// remain absolute so page arrivals cannot reset the visible selection.
type View struct {
	Title                         string
	Location                      jellyfin.Location
	Page                          jellyfin.Page
	Start, Selected, PendingStart int
	Scroll, Target                int
	Loading                       bool
	Error                         string
	Detail                        *jellyfin.Item
	fetching, prefetchFailed      bool
	direction                     int
}
type Request struct {
	Generation int
	Location   jellyfin.Location
	Start      int
}

// Model owns navigation, the music queue screen, and photo control visibility.
// PlaybackController owns decoder state separately. Only the browser loop mutates Model.
type Model struct {
	Stack                       []View
	Generation                  int
	Rows                        int
	ListMode, ExitConfirm, Quit bool
	Notice                      string
	musicQueue                  bool
	photoControlsUntil          time.Time
}

func New() *Model {
	return &Model{Rows: 6, Stack: []View{{Title: "Libraries", Location: jellyfin.Location{Kind: "views"}}}}
}
func (m *Model) Current() *View { return &m.Stack[len(m.Stack)-1] }

// Load requests a page needed for navigation or explicit retry. Existing rows
// remain visible. Prefetch uses the same request path without setting Loading.
func (m *Model) Load(start int) *Request {
	m.Generation++
	v := m.Current()
	v.Loading = true
	v.fetching = true
	v.prefetchFailed = false
	v.Error = ""
	v.PendingStart = start
	return &Request{m.Generation, v.Location, start}
}

// Apply accepts only the active listing request. A foreground result selects
// the waiting target. A background result preserves the user's current item.
func (m *Model) Apply(req Request, page jellyfin.Page, err error) bool {
	if req.Generation != m.Generation {
		return false
	}
	v := m.Current()
	waiting := v.Loading
	v.Loading, v.fetching = false, false
	if err == nil && len(page.Items) == 0 && req.Start == v.Start+len(v.Page.Items) && v.Page.TotalRecordCount == nil {
		// A full final page without a total needs one empty response to find
		// the end. Keep the rows and stop requesting that nonexistent page.
		total := req.Start
		v.Page.TotalRecordCount = &total
		v.Target = v.Start + v.Selected
		v.centerSelection(m.Rows)
		v.Error = ""
		return true
	}
	if err != nil || (len(page.Items) == 0 && req.Start > 0) {
		v.prefetchFailed = true
		if waiting {
			if err != nil {
				v.Error = err.Error()
			} else {
				v.Error = "No items returned for this page. Press R to retry."
			}
		}
		return true
	}
	target := v.Start + v.Selected
	if waiting {
		target = v.Target
	}
	v.retainPage(req.Start, page, target, m.Rows)
	v.Error = ""
	v.prefetchFailed = false
	return true
}
func (v *View) More() bool {
	if v.Page.TotalRecordCount != nil {
		return v.Start+len(v.Page.Items) < *v.Page.TotalRecordCount
	}
	return len(v.Page.Items) > 0 && len(v.Page.Items)%PageSize == 0
}
func (v *View) Item() *jellyfin.Item {
	if v.Detail != nil {
		return v.Detail
	}
	if v.Selected < 0 || v.Selected >= len(v.Page.Items) {
		return nil
	}
	return &v.Page.Items[v.Selected]
}
func (m *Model) Key(key string) *Request {
	v := m.Current()
	if m.Notice != "" {
		if key == "back" || key == "open" {
			m.Notice = ""
		}
		return nil
	}
	if m.ExitConfirm {
		if key == "open" {
			m.Quit = true
		}
		if key == "back" {
			m.ExitConfirm = false
		}
		return nil
	}
	if key == "select" && len(m.Stack) == 1 {
		m.ListMode = !m.ListMode
		return nil
	}
	if key == "back" {
		if m.ReturnToParent() {
			return nil
		}
		m.Generation++
		if v.Loading {
			v.Error = "Loading canceled. Press R to retry."
		}
		if len(m.Stack) == 1 && v == m.Current() && !v.Loading {
			m.ExitConfirm = true
		}
		m.Current().Loading = false
		return nil
	}
	if key == "retry" {
		if v.Detail != nil {
			return nil
		}
		return m.Load(v.PendingStart)
	}
	if v.Detail != nil && v.Detail.Type == "Photo" && key == "open" {
		m.photoControlsUntil = time.Time{}
		return nil
	}
	if v.Detail != nil && key == "open" {
		m.Notice = "Playback for this item type is not available yet.  A:back"
	}
	if (v.Loading && len(v.Page.Items) == 0) || v.Detail != nil {
		return nil
	}
	if key == "up" || key == "down" || key == "next" || key == "previous" {
		step := 1
		if key == "up" || key == "previous" {
			step = -1
		}
		if len(m.Stack) == 1 && !m.ListMode {
			if key == "up" || key == "down" {
				return nil
			}
		} else if key == "next" || key == "previous" {
			step *= max(1, m.Rows)
		}
		v.direction = step
		target := max(0, v.Start+v.Selected+step)
		if v.Page.TotalRecordCount != nil {
			target = min(target, max(0, *v.Page.TotalRecordCount-1))
		} else if !v.More() {
			target = min(target, v.Start+len(v.Page.Items)-1)
		}
		if target < v.Start || target >= v.Start+len(v.Page.Items) {
			v.Target = target
			start := target / PageSize * PageSize
			if v.fetching && v.PendingStart == start {
				v.Loading = true
				return nil
			}
			return m.Load(start)
		}
		v.Selected = max(0, target-v.Start)
		v.Target = target
		v.Loading = false
		v.Error = ""
		v.centerSelection(m.Rows)
		return nil
	}
	switch key {
	case "open":
		if v.Loading {
			return nil
		}
		item := v.Item()
		if item == nil {
			return nil
		}
		next := View{Title: item.Name, Location: jellyfin.Location{Kind: "items", ParentID: item.ID, Collection: v.Location.Collection, SeriesID: v.Location.SeriesID}}
		switch {
		case v.Location.Kind == "views":
			next.Location.Collection = item.CollectionType
			if item.CollectionType == "livetv" {
				next.Location.Kind = "livetv"
			}
		case item.Type == "Series":
			next.Location.Kind = "seasons"
			next.Location.SeriesID = item.ID
		case item.Type == "Season":
			next.Title = v.Title + " / " + item.Name
			next.Location.Kind = "episodes"
			if item.SeriesID != "" {
				next.Location.SeriesID = item.SeriesID
			}
		case item.Type == "MusicAlbum":
			next.Title = v.Title + " / " + item.Name
		case item.IsFolder || item.Type == "Folder" || item.Type == "PhotoAlbum" || item.Type == "MusicArtist" || item.Type == "MusicAlbum" || item.Type == "BoxSet" || item.Type == "Playlist":
		default:
			copy := *item
			next.Detail = &copy
		}
		if len(m.Stack) >= 32 {
			v.Error = "Maximum folder depth reached"
			return nil
		}
		m.photoControlsUntil = time.Time{}
		m.Generation++
		v.fetching = false
		m.Stack = append(m.Stack, next)
		if next.Detail == nil {
			return m.Load(0)
		}
	}
	return nil
}
func (v *View) Count() string {
	if len(v.Page.Items) == 0 {
		return "0 items"
	}
	total := "?"
	if v.Page.TotalRecordCount != nil {
		total = fmt.Sprint(*v.Page.TotalRecordCount)
	}
	return fmt.Sprintf("%d/%s", v.Start+v.Selected+1, total)
}
