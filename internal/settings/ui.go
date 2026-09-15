package settings

import (
	"encoding/json"
	"strings"
	"unicode"
)

// UI separates title errors from navigation sound input. An invalid whole
// object affects both. An invalid title value does not change sound settings.
type UI struct {
	Title            *string
	TitleError       error
	NavigationSounds Section
}

// ParseUI owns the recognized UI fields and heading normalization. Nil title
// selects the default. An explicit empty title hides it. Sound values are left
// to the sound package, so their errors cannot replace a valid heading.
func ParseUI(source Section) UI {
	var fields map[string]json.RawMessage
	if err := source.Decode(&fields); err != nil {
		return UI{TitleError: err, NavigationSounds: Section{Path: source.Path, Err: err}}
	}
	var values struct {
		Title            *string         `json:"title"`
		NavigationSounds json.RawMessage `json:"navigation_sounds"`
	}
	result := UI{TitleError: source.Decode(&values), NavigationSounds: Section{Path: source.Path, Data: fields["navigation_sounds"]}}
	if result.TitleError != nil || values.Title == nil {
		return result
	}
	title := strings.Join(strings.Fields(*values.Title), " ")
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
