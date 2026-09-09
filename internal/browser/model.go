// Package browser owns navigation independently of C, hardware, and HTTP.
package browser

import (
	"fmt"
	"misterfin-go/internal/jellyfin"
)

const PageSize = 64

type View struct {
	Title                         string
	Location                      jellyfin.Location
	Page                          jellyfin.Page
	Start, Selected, PendingStart int
	Scroll, Target                int
	Loading                       bool
	Error                         string
	Detail                        *jellyfin.Item
}
type Request struct {
	Generation int
	Location   jellyfin.Location
	Start      int
}
type Model struct {
	Stack                       []View
	Generation                  int
	Rows                        int
	ListMode, ExitConfirm, Quit bool
	Notice                      string
}

func New() *Model {
	return &Model{Rows: 6, Stack: []View{{Title: "Libraries", Location: jellyfin.Location{Kind: "views"}}}}
}
func (m *Model) Current() *View { return &m.Stack[len(m.Stack)-1] }
func (m *Model) Load(start int) *Request {
	m.Generation++
	v := m.Current()
	v.Loading = true
	v.Error = ""
	v.PendingStart = start
	return &Request{m.Generation, v.Location, start}
}
func (m *Model) Apply(req Request, page jellyfin.Page, err error) bool {
	if req.Generation != m.Generation {
		return false
	}
	v := m.Current()
	v.Loading = false
	if err != nil {
		v.Error = err.Error()
		return true
	}
	// A contradictory count must not replace the previous page with a blank one.
	if len(page.Items) == 0 && req.Start > 0 {
		v.Error = "No items returned for this page. Press R to retry."
		return true
	}
	v.Page = page
	v.Start = req.Start
	v.Selected = min(max(0, v.Target-req.Start), max(0, len(page.Items)-1))
	v.Scroll = max(0, v.Selected-m.Rows+1)
	v.Error = ""
	return true
}
func (v *View) More() bool {
	if v.Page.TotalRecordCount != nil {
		return v.Start+len(v.Page.Items) < *v.Page.TotalRecordCount
	}
	return len(v.Page.Items) == PageSize
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
		m.Generation++
		if len(m.Stack) > 1 {
			m.Stack = m.Stack[:len(m.Stack)-1]
		} else if v.Loading {
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
	if v.Detail != nil && key == "open" {
		m.Notice = "Playback for this item type is not available yet.  A:back"
	}
	if v.Loading || v.Detail != nil {
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
		target := max(0, v.Start+v.Selected+step)
		if v.Page.TotalRecordCount != nil {
			target = min(target, max(0, *v.Page.TotalRecordCount-1))
		} else if !v.More() {
			target = min(target, v.Start+len(v.Page.Items)-1)
		}
		if target < v.Start || target >= v.Start+len(v.Page.Items) {
			v.Target = target
			return m.Load(target / PageSize * PageSize)
		}
		v.Selected = max(0, target-v.Start)
		if v.Selected < v.Scroll {
			v.Scroll = v.Selected
		}
		if v.Selected >= v.Scroll+max(1, m.Rows) {
			v.Scroll = v.Selected - max(1, m.Rows) + 1
		}
		return nil
	}
	switch key {
	case "open":
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
