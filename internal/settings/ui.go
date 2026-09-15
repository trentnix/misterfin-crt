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
	// Decode the schema before validating either value. Unknown fields invalidate
	// the whole object, but a mistyped title must not change valid sound settings.
	var values struct {
		Title            json.RawMessage `json:"title"`
		NavigationSounds json.RawMessage `json:"navigation_sounds"`
	}
	if err := source.Decode(&values); err != nil {
		return UI{TitleError: err, NavigationSounds: Section{Path: source.Path, Err: err}}
	}
	result := UI{NavigationSounds: Section{Path: source.Path, Data: values.NavigationSounds}}
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
