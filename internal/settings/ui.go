package settings

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"
)

// UI validates heading and carousel options independently of navigation sounds.
// An invalid whole object resets all fields. Invalid field values affect only
// that field. Nil visibility options show nonempty categories by default.
type UI struct {
	Title                                    *string
	TitleError                               error
	NavigationSounds                         Section
	ShowCollections, ShowPlaylists           *bool
	ShowCollectionsError, ShowPlaylistsError error
}

// ParseUI owns the recognized UI fields and heading normalization. Nil title
// selects the default. An explicit empty title hides it. Visibility defaults to
// showing nonempty collections and playlists. Sound values are left
// to the sound package, so their errors cannot replace a valid heading.
func ParseUI(source Section) UI {
	// Decode the schema before validating individual values. Unknown fields
	// invalidate the whole object. Field errors preserve other valid settings.
	var values struct {
		Title            json.RawMessage `json:"title"`
		NavigationSounds json.RawMessage `json:"navigation_sounds"`
		ShowCollections  json.RawMessage `json:"show_collections"`
		ShowPlaylists    json.RawMessage `json:"show_playlists"`
	}
	if err := source.Decode(&values); err != nil {
		return UI{TitleError: err, NavigationSounds: Section{Path: source.Path, Err: err}, ShowCollectionsError: err, ShowPlaylistsError: err}
	}
	result := UI{NavigationSounds: Section{Path: source.Path, Data: values.NavigationSounds}}
	result.ShowCollections, result.ShowCollectionsError = optionalBool(values.ShowCollections)
	result.ShowPlaylists, result.ShowPlaylistsError = optionalBool(values.ShowPlaylists)
	if values.Title == nil {
		return result
	}
	var value *string
	result.TitleError = json.Unmarshal(values.Title, &value)
	if result.TitleError != nil || value == nil {
		return result
	}
	title := strings.Join(strings.Fields(*value), " ")
	title = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, title)
	title = strings.TrimSpace(title)
	result.Title = &title
	return result
}

// optionalBool distinguishes an omitted default from an explicit boolean.
// Null and other types are invalid and fall back without retaining a value.
func optionalBool(data json.RawMessage) (*bool, error) {
	if data == nil {
		return nil, nil
	}
	var value *bool
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, errors.New("setting must be true or false")
	}
	return value, nil
}
