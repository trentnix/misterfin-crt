// Package browser coordinates media-server navigation and media controls. Run owns
// the event loop, Model tracks navigation, and PlaybackController tracks decoder
// transitions. The browser supplies read-only scenes to rendering.Renderer and
// presents its frames through videoout.Output.
package browser

import (
	"time"

	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/media"
)

// PageSize is the requested number of items per library page.
const PageSize = 64

// View retains one navigation screen. Start is the absolute index of the first
// retained row. Selected and Scroll index that window. Target and PendingStart
// remain absolute so page arrivals cannot reset the visible selection.
type View struct {
	Title                         string
	Location                      media.Location
	Page                          media.Page
	Start, Selected, PendingStart int
	Scroll, Target                int
	Loading                       bool
	Error                         string
	Detail                        *media.Item
	fetching, prefetchFailed      bool
	direction                     int
}

// Request identifies a listing operation. Generation lets Apply reject results
// from a superseded request. Start is an absolute, zero-based item index.
type Request struct {
	Generation int
	Location   media.Location
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

// New creates a model at the library carousel with a six-row list viewport.
// Startup sets Rows to the renderer's capacity for the selected display.
func New() *Model {
	return &Model{Rows: 6, Stack: []View{{Title: "Libraries", Location: media.Location{Kind: "views"}}}}
}

// Current borrows the active view. Models created by New always have one.
// The pointer must not be retained across navigation that changes Stack.
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
func (m *Model) Apply(req Request, page media.Page, err error) bool {
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
				v.Error = "No items returned for this page."
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

// More reports whether another page may follow the retained items. Without a
// server total, a full page remains a possible continuation until an empty reply.
func (v *View) More() bool {
	if v.Page.TotalRecordCount != nil {
		return v.Start+len(v.Page.Items) < *v.Page.TotalRecordCount
	}
	return len(v.Page.Items) > 0 && len(v.Page.Items)%PageSize == 0
}

// Item borrows the detail item or selected list item, or returns nil when empty.
// The pointer must not be retained across page replacement or navigation.
func (v *View) Item() *media.Item {
	if v.Detail != nil {
		return v.Detail
	}
	if v.Selected < 0 || v.Selected >= len(v.Page.Items) {
		return nil
	}
	return &v.Page.Items[v.Selected]
}

// Key applies a normalized browsing action and returns any required page request.
// The browser handles media controls and filters repeats before calling Key.
func (m *Model) Key(key control.Action) *Request {
	v := m.Current()
	if m.Notice != "" {
		if key == control.Back || key == control.Open {
			m.Notice = ""
		}
		return nil
	}
	if m.ExitConfirm {
		if key == control.Open {
			m.Quit = true
		}
		if key == control.Back {
			m.ExitConfirm = false
		}
		return nil
	}
	if key == control.Select && len(m.Stack) == 1 {
		m.ListMode = !m.ListMode
		return nil
	}
	if key == control.Back {
		if m.ReturnToParent() {
			return nil
		}
		m.Generation++
		if v.Loading {
			v.Error = "Loading canceled."
		}
		if len(m.Stack) == 1 && v == m.Current() && !v.Loading {
			m.ExitConfirm = true
		}
		m.Current().Loading = false
		return nil
	}
	if key == control.Retry {
		if v.Detail != nil {
			return nil
		}
		return m.Load(v.PendingStart)
	}
	if v.Detail != nil && v.Detail.Type == "Photo" && key == control.Open {
		m.photoControlsUntil = time.Time{}
		return nil
	}
	if v.Detail != nil && key == control.Open {
		m.Notice = "Playback for this item type is not available yet.  A:back"
	}
	if (v.Loading && len(v.Page.Items) == 0) || v.Detail != nil {
		return nil
	}
	if key == control.Up || key == control.Down || key == control.Next || key == control.Previous {
		step := 1
		if key == control.Up || key == control.Previous {
			step = -1
		}
		if len(m.Stack) == 1 && !m.ListMode {
			if key == control.Up || key == control.Down {
				return nil
			}
		} else if key == control.Next || key == control.Previous {
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
	case control.Open:
		if v.Loading {
			return nil
		}
		item := v.Item()
		if item == nil {
			return nil
		}
		next := View{Title: item.Name, Location: media.Location{Kind: "items", ParentID: item.ID, Collection: v.Location.Collection, SeriesID: v.Location.SeriesID}}
		switch {
		case item.ID == continueID:
			next.Title = "Continue Watching"
			next.Location = media.Location{Kind: "continue"}
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
