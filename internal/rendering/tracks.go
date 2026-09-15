package rendering

// TrackRow is one display choice. Index is a server stream index or picture mode.
type TrackRow struct {
	Index  int
	Label  string
	Active bool
}

// TrackMenu is an immutable render snapshot, independent of the input device.
type TrackMenu struct {
	Tab, Selected int
	Rows          []TrackRow
	Delay         string
	Message       string
}
