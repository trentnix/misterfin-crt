package plex

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"misterfin-crt/internal/media"
)

// identifier accepts Plex IDs encoded as either strings or JSON numbers.
type identifier string

func (id *identifier) UnmarshalJSON(data []byte) error {
	var text string
	if len(data) > 0 && data[0] == '"' {
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
	} else {
		var n json.Number
		if err := json.Unmarshal(data, &n); err != nil {
			return err
		}
		text = n.String()
	}
	*id = identifier(text)
	return nil
}

type containerResponse struct {
	Container *container `json:"MediaContainer"`
}
type container struct {
	Size        int        `json:"size"`
	Total       *int       `json:"totalSize"`
	Offset      int        `json:"offset"`
	Directories []metadata `json:"Directory"`
	Metadata    []metadata `json:"Metadata"`
	Photos      []metadata `json:"Photo"`
}

// entries normalizes Plex's item containers. Directory records with type photo
// are albums. Navigation shortcuts without a ratingKey are not media items.
func (c *container) entries() []metadata {
	entries := append([]metadata(nil), c.Metadata...)
	for _, entry := range c.Directories {
		if entry.ID == "" {
			continue
		}
		if entry.Type == "photo" {
			entry.Type = "photoalbum"
		}
		entries = append(entries, entry)
	}
	return append(entries, c.Photos...)
}

type metadata struct {
	ID               identifier `json:"ratingKey"`
	Key              string     `json:"key"`
	Title            string     `json:"title"`
	Type             string     `json:"type"`
	Summary          string     `json:"summary"`
	Year             int        `json:"year"`
	Duration         int64      `json:"duration"`
	ViewOffset       int64      `json:"viewOffset"`
	ViewCount        int        `json:"viewCount"`
	LastViewedAt     int64      `json:"lastViewedAt"`
	Index            *int       `json:"index"`
	ParentIndex      *int       `json:"parentIndex"`
	ParentID         identifier `json:"parentRatingKey"`
	GrandparentID    identifier `json:"grandparentRatingKey"`
	ParentTitle      string     `json:"parentTitle"`
	GrandparentTitle string     `json:"grandparentTitle"`
	ChildCount       int        `json:"childCount"`
	LeafCount        int        `json:"leafCount"`
	Rating           float64    `json:"rating"`
	Thumb            string     `json:"thumb"`
	Art              string     `json:"art"`
	ParentThumb      string     `json:"parentThumb"`
	GrandparentThumb string     `json:"grandparentThumb"`
	Media            []version  `json:"Media"`
}

// UnmarshalJSON distinguishes Plex's scalar rating from its Rating records.
// encoding/json otherwise matches both keys to Rating without regard to case.
// Provider rating records are not used by the shared item model.
func (m *metadata) UnmarshalJSON(data []byte) error {
	type fields metadata
	var decoded struct {
		*fields
		Ratings json.RawMessage `json:"Rating"`
	}
	decoded.fields = (*fields)(m)
	return json.Unmarshal(data, &decoded)
}

type version struct {
	ID            identifier `json:"id"`
	Width, Height int
	AspectRatio   float64 `json:"aspectRatio"`
	Part          []part  `json:"Part"`
}
type part struct {
	ID      identifier `json:"id"`
	Key     string     `json:"key"`
	Streams []stream   `json:"Stream"`
}
type stream struct {
	ID                     int `json:"id"`
	Type                   int `json:"streamType"`
	Codec, Language, Title string
	DisplayTitle           string `json:"displayTitle"`
	Default, Forced        bool
	Selected               bool
	Key                    string
	Width, Height          int
}

func (m metadata) item() media.Item {
	item := media.Item{ID: string(m.ID), Name: m.Title, Overview: m.Summary, ProductionYear: m.Year, RunTimeTicks: m.Duration * 10000, IndexNumber: m.Index, ParentIndexNumber: m.ParentIndex, ChildCount: m.ChildCount, RecursiveItemCount: m.LeafCount, CommunityRating: m.Rating, ImageTags: make(map[string]string)}
	item.Type = map[string]string{"movie": "Movie", "show": "Series", "season": "Season", "episode": "Episode", "clip": "Video", "artist": "MusicArtist", "album": "MusicAlbum", "track": "Audio", "photo": "Photo", "photoalbum": "PhotoAlbum"}[m.Type]
	// JSON Metadata can also encode albums as photo with a children endpoint.
	if m.Type == "photo" && strings.HasSuffix(m.Key, "/children") {
		item.Type = "PhotoAlbum"
	}
	item.IsFolder = item.Type == "PhotoAlbum" || m.Type == "show" || m.Type == "season" || m.Type == "artist" || m.Type == "album"
	if item.Type == "" {
		item.Type = "Folder"
		item.IsFolder = true
	}
	item.UserData.PlaybackPositionTicks = max(0, m.ViewOffset) * 10000
	item.UserData.Played = m.ViewCount > 0 && m.ViewOffset == 0
	if m.LastViewedAt > 0 {
		at := time.Unix(m.LastViewedAt, 0)
		item.UserData.LastPlayedDate = &at
	}
	thumb := m.Thumb
	if item.Type == "Photo" {
		// Resize the original photo through Plex instead of enlarging a thumbnail
		// or downloading a full-resolution image into the framebuffer client.
		for _, v := range m.Media {
			if len(v.Part) == 1 && v.Part[0].Key != "" {
				thumb = v.Part[0].Key
				break
			}
		}
	}
	if thumb == "" && item.Type != "Photo" {
		thumb = m.ParentThumb
	}
	if thumb == "" && item.Type != "Photo" {
		thumb = m.GrandparentThumb
	}
	if thumb != "" {
		item.ImageTags["Primary"] = thumb
	}
	if m.Art != "" {
		item.BackdropImageTags = []string{m.Art}
	}
	switch m.Type {
	case "track":
		item.Album = m.ParentTitle
		if m.GrandparentTitle != "" {
			item.Artists = []string{m.GrandparentTitle}
		}
	case "album":
		if m.ParentTitle != "" {
			item.Artists = []string{m.ParentTitle}
		}
	case "episode":
		item.SeriesID = string(m.GrandparentID)
		item.SeriesName = m.GrandparentTitle
	case "season":
		item.SeriesID = string(m.ParentID)
		item.SeriesName = m.ParentTitle

	}
	for i, v := range m.Media {
		// Multi-file movies need an explicit handoff policy before exposure.
		if len(v.Part) != 1 {
			continue
		}
		p := v.Part[0]
		source := media.MediaSource{ID: fmt.Sprintf("%d:%s", i, p.ID)}
		for _, s := range p.Streams {
			kind := map[int]string{1: "Video", 2: "Audio", 3: "Subtitle"}[s.Type]
			if kind == "" {
				continue
			}
			track := media.MediaStream{Type: kind, Index: s.ID, Codec: s.Codec, Language: s.Language, Title: s.Title, DisplayTitle: s.DisplayTitle, IsDefault: s.Default || s.Selected, IsForced: s.Forced, IsExternal: s.Key != "", Width: s.Width, Height: s.Height}
			// Plex exports sidecar text, but embedded subtitles require server
			// rendering. Preserve the codec and advertise that delivery constraint.
			track.RequiresBurnIn = kind == "Subtitle" && s.Key == ""
			if kind == "Video" {
				if track.Width == 0 {
					track.Width = v.Width
					track.Height = v.Height
				}
				if v.AspectRatio > 0 {
					track.AspectRatio = strconv.FormatFloat(v.AspectRatio, 'f', -1, 64)
				}
			}
			source.MediaStreams = append(source.MediaStreams, track)
		}
		item.MediaSources = append(item.MediaSources, source)
	}
	if len(item.MediaSources) > 0 {
		item.MediaStreams = item.MediaSources[0].MediaStreams
	}
	return item
}

func validID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
func sectionID(id string) (string, bool) {
	value, ok := strings.CutPrefix(id, "library:")
	return value, ok && validID(value)
}
