package terminal

import (
	"strconv"
	"strings"
)

func characterAction(c rune) string {
	return map[rune]string{57364: "about", 9: "select", 27: "back", 'q': "quit", 'Q': "quit", 'a': "back", 'A': "back", 'z': "back", 'Z': "back", 127: "back", 8: "back", 'b': "open", 'B': "open", 'x': "open", 'X': "open", 13: "open", 10: "open", 'r': "retry", 'R': "retry", 'j': "seek-backward", 'J': "seek-backward", 'l': "seek-forward", 'L': "seek-forward", '[': "track-previous", ']': "track-next", 'k': "up"}[c]
}

// sequenceAction accepts legacy cursor keys and Kitty keyboard event types.
// Repeats retain their identity so the browser can repeat scrolling and seeking
// while keeping menu toggles and track changes restricted to press events.
func sequenceAction(seq string) string {
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
	var key string
	if final == 'u' {
		code, err := strconv.Atoi(strings.Split(fields[0], ":")[0])
		if err != nil {
			return ""
		}
		if code == 99 && modifier == 5 {
			return "quit"
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
			key = map[byte]string{'A': "up", 'B': "down", 'C': "next", 'D': "previous"}[final]
		case 'P':
			if fields[0] == "" || fields[0] == "1" {
				key = "about"
			}
		case '~':
			key = map[string]string{"11": "about", "5": "track-previous", "6": "track-next"}[fields[0]]
		}
	}
	if key != "" && event == 2 {
		key += "-repeat"
	}
	return key
}
