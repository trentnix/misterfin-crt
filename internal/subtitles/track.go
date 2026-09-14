// Package subtitles parses timed text for the shared playback overlay.
package subtitles

import (
	"errors"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Cue holds absolute source times in Jellyfin ticks and plain display text.
type Cue struct {
	Start, End int64
	Text       string
}

// Track is an immutable cue index. It can be shared between decoder and UI loops.
type Track struct {
	cues []Cue
	ends []int64
}

var timing = regexp.MustCompile(`^(\d{1,3}):(\d{2}):(\d{2})[,.](\d{3})\s+-->\s+(\d{1,3}):(\d{2}):(\d{2})[,.](\d{3})(?:\s.*)?$`)
var markup = regexp.MustCompile(`(?i)</?(?:i|b|u)>|</?font(?:\s[^>\n]*)?>|\{\\[^}\n]*\}`)
var breaks = regexp.MustCompile(`(?i)<br\s*/?>`)

// Parse accepts SubRip exported by Jellyfin, including ASS escapes and markup.
// Malformed cue blocks are skipped. Entirely invalid or oversized files fail.
func Parse(data []byte) (*Track, error) {
	if len(data) > 4<<20 {
		return nil, errors.New("subtitle exceeds 4 MiB")
	}
	text := strings.TrimPrefix(strings.ReplaceAll(string(data), "\r\n", "\n"), "\ufeff")
	lines := strings.Split(text, "\n")
	t := &Track{}
	for i := 0; i < len(lines); i++ {
		match := timing.FindStringSubmatch(strings.TrimSpace(lines[i]))
		if match == nil {
			continue
		}
		ticks := func(values []string) int64 {
			v := make([]int64, 4)
			for j, s := range values {
				v[j], _ = strconv.ParseInt(s, 10, 64)
			}
			if v[1] > 59 || v[2] > 59 {
				return -1
			}
			return ((v[0]*3600+v[1]*60+v[2])*1000 + v[3]) * 10000
		}
		start, end := ticks(match[1:5]), ticks(match[5:9])
		var body []string
		for i++; i < len(lines) && strings.TrimSpace(lines[i]) != ""; i++ {
			body = append(body, lines[i])
		}
		value := PlainText(strings.Join(body, "\n"))
		if start >= 0 && end > start && value != "" {
			t.cues = append(t.cues, Cue{start, end, value})
		}
		if len(t.cues) > 20000 {
			return nil, errors.New("subtitle has too many cues")
		}
	}
	if len(t.cues) == 0 {
		return nil, errors.New("subtitle contains no timed text")
	}
	sort.SliceStable(t.cues, func(i, j int) bool { return t.cues[i].Start < t.cues[j].Start })
	end := int64(0)
	for _, cue := range t.cues {
		end = max(end, cue.End)
		t.ends = append(t.ends, end)
	}
	return t, nil
}

// At returns all active cues, including overlapping dialogue, at an absolute time.
func (t *Track) At(ticks int64) string {
	if t == nil {
		return ""
	}
	first := sort.Search(len(t.ends), func(i int) bool { return t.ends[i] > ticks })
	var lines []string
	for i := first; i < len(t.cues) && t.cues[i].Start <= ticks; i++ {
		if len(lines) >= 3 {
			break
		}
		if ticks < t.cues[i].End {
			lines = append(lines, t.cues[i].Text)
		}
	}
	return strings.Join(lines, "\n")
}

// PlainText removes SubRip markup and ASS styling while preserving line breaks.
// Output is limited to 2,048 runes for the shared subtitle and caption overlay.
func PlainText(value string) string {
	value = breaks.ReplaceAllString(value, "\n")
	value = markup.ReplaceAllString(value, "")
	value = strings.NewReplacer(`\N`, "\n", `\n`, "\n", `\h`, " ").Replace(value)
	value = strings.TrimSpace(html.UnescapeString(value))
	if runes := []rune(value); len(runes) > 2048 {
		value = string(runes[:2048])
	}
	return value
}
