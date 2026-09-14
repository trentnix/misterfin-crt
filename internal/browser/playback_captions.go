package browser

// captionState owns the latest decoder screen and the viewer's local selection.
// Decoding continues while Off so enabling captions can show the current text.
// A new channel starts Off. Caption updates never dismiss or open controls.
type captionState struct {
	available, enabled bool
	text               string
}

// rows exposes only the primary EIA-608 service supported by our decoders.
func (c captionState) rows() []TrackRow {
	rows := []TrackRow{{Index: -1, Label: "Off", Active: !c.enabled}}
	if c.available {
		rows = append(rows, TrackRow{Index: 0, Label: "Closed captions", Active: c.enabled})
	}
	return rows
}
