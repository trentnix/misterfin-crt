package jellyfin

import "time"

// MediaStream describes source video geometry returned by Jellyfin.
type MediaStream struct {
	Type, AspectRatio string
	Width, Height     int
}

// Item contains metadata shared by library, detail, and playback endpoints.
// Available fields depend on the query. Durations and positions use Jellyfin
// ticks of 100 nanoseconds. Images are identified by tags and fetched separately.
type Item struct {
	ID                                             string `json:"Id"`
	Name, Type, CollectionType, SeriesID, Overview string
	SeriesName                                     string
	IndexNumber, ParentIndexNumber                 *int
	// ContinueAction is local presentation metadata, never sent to Jellyfin.
	ContinueAction                 string `json:"-"`
	IsFolder                       bool
	ProductionYear                 int
	RunTimeTicks                   int64
	ChildCount, RecursiveItemCount int
	CommunityRating                float64
	BackdropImageTags              []string
	ImageTags                      map[string]string
	ParentBackdropItemId           string
	ParentBackdropImageTags        []string
	Number, ChannelNumber          string
	CurrentProgram                 struct{ Name string }
	MediaStreams                   []MediaStream
	UserData                       struct {
		LastPlayedDate        *time.Time
		Played                bool
		PlaybackPositionTicks int64
	}
}

// Page is an item listing. A nil TotalRecordCount means the server omitted the
// total. A pointer to zero represents a known empty result.
type Page struct {
	Items            []Item
	TotalRecordCount *int
}

// Location identifies a browsing query. Kind selects views, items, seasons,
// episodes, or livetv. ParentID identifies the folder or season. SeriesID is
// required for season and episode queries. Collection selects C-compatible fields.
// The browser owns the synthetic "continue" location and never sends it to List.
type Location struct{ Kind, ParentID, Collection, SeriesID string }
