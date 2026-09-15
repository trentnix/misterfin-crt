package terminal

import (
	"strconv"
	"strings"

	"misterfin-crt/internal/input/control"
)

// characterAction maps terminal character codes to shared semantic actions.
func characterAction(c rune) control.Action {
	switch c {
	case 57364:
		return control.About
	case 9:
		return control.Select
	case 27, 'a', 'A', 'z', 'Z', 127, 8:
		return control.Back
	case 'q', 'Q':
		return control.Quit
	case 'b', 'B', 'x', 'X', 13, 10:
		return control.Open
	case 'r', 'R':
		return control.Retry
	case 'j', 'J':
		return control.SeekBackward
	case 'l', 'L':
		return control.SeekForward
	case '[':
		return control.TrackPrevious
	case ']':
		return control.TrackNext
	case 'k':
		return control.Up
	}
	return ""
}

// sequenceAction accepts legacy cursor keys and Kitty keyboard event types.
// Repeats retain their identity so the browser can repeat scrolling and seeking
// while keeping menu toggles and track changes restricted to press events.
func sequenceAction(seq string) control.Action {
	if seq == "" {
		return ""
	}
	final := seq[len(seq)-1]
	fields := strings.Split(seq[:len(seq)-1], ";")
	modifier, event := 1, 1
	if len(fields) > 1 {
		parts := strings.Split(fields[1], ":")
		if parts[0] != "" {
			modifier, _ = strconv.Atoi(parts[0])
		}
		if len(parts) > 1 {
			event, _ = strconv.Atoi(parts[1])
		}
	}
	if event != 1 && event != 2 {
		return ""
	}
	var key control.Action
	if final == 'u' {
		code, err := strconv.Atoi(strings.Split(fields[0], ":")[0])
		if err != nil {
			return ""
		}
		if code == 99 && modifier == 5 {
			return control.Quit
		} // Ctrl+C in enhanced mode
		if modifier != 1 && modifier != 2 {
			return ""
		}
		key = characterAction(rune(code))
	} else {
		if modifier != 1 {
			return ""
		}
		switch final {
		case 'A', 'B', 'C', 'D':
			if fields[0] != "" && fields[0] != "1" {
				return ""
			}
			key = map[byte]control.Action{'A': control.Up, 'B': control.Down, 'C': control.Next, 'D': control.Previous}[final]
		case 'P':
			if fields[0] == "" || fields[0] == "1" {
				key = control.About
			}
		case '~':
			key = map[string]control.Action{"11": control.About, "5": control.TrackPrevious, "6": control.TrackNext}[fields[0]]
		}
	}
	if key != "" && event == 2 {
		key = key.Repeat()
	}
	return key
}
