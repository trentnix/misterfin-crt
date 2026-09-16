// Package media defines the shared library and playback model. Server adapters
// translate their API responses into these values before presenting them.
package media

import "time"

// MediaStream describes a video, audio, or subtitle stream returned by a media server.
type MediaStream struct {
	Type, AspectRatio                    string
	Index                                int
	Codec, Language, Title, DisplayTitle string
	IsDefault, IsForced, IsExternal      bool
	Width, Height                        int
}

// MediaSource identifies the file whose stream indexes server exposes.
type MediaSource struct {
	ID           string `json:"Id"`
	MediaStreams []MediaStream
}

// Item contains metadata shared by library, detail, and playback endpoints.
// Available fields depend on the query. Durations and positions use shared
// ticks of 100 nanoseconds. Images are identified by tags and fetched separately.
type Item struct {
	ID                                             string `json:"Id"`
	Name, Type, CollectionType, SeriesID, Overview string
	SeriesName                                     string
	Artists                                        []string
	Album                                          string
	IndexNumber, ParentIndexNumber                 *int
	// ContinueAction is local presentation metadata, never sent to a server.
	ContinueAction                 string `json:"-"`
	IsFolder                       bool
	ProductionYear                 int
	RunTimeTicks                   int64
	ChildCount, RecursiveItemCount int
	CommunityRating                float64
	BackdropImageTags              []string
	ImageTags                      map[string]string
	ParentBackdropItemID           string `json:"ParentBackdropItemId"`
	ParentBackdropImageTags        []string
	Number, ChannelNumber          string
	CurrentProgram                 struct{ Name string }
	MediaStreams                   []MediaStream
	MediaSources                   []MediaSource
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
// required for season and episode queries. Collection identifies the library category.
// The browser owns the synthetic "continue" location and never sends it to List.
type Location struct{ Kind, ParentID, Collection, SeriesID string }
