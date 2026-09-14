package playback

import (
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"

	"misterfin-crt/internal/subtitles"
)

// parseCaption accepts bounded UTF-8 screen snapshots from video decoders.
// Empty text clears the caption. Native FFmpeg text may contain ASS styling.
func parseCaption(line string) (string, bool) {
	payload, plain := strings.CutPrefix(line, "ANS_CAPTION_TEXT=")
	if !plain {
		var ok bool
		payload, ok = strings.CutPrefix(line, "ANS_CAPTION_ASS=")
		if !ok {
			return "", false
		}
	}
	if len(payload) > 4096 {
		return "", false
	}
	data, err := hex.DecodeString(payload)
	if err != nil || !utf8.Valid(data) {
		return "", false
	}
	text := string(data)
	if !plain {
		text = subtitles.PlainText(text)
	}
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' {
			return -1
		}
		return r
	}, text)
	return strings.TrimSpace(text), true
}

// publishCaption keeps the newest complete screen, including clear events.
// Decoder output must not wait for the UI to consume preceding updates.
func publishCaption(ch chan string, text string) {
	select {
	case ch <- text:
		return
	default:
	}
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- text:
	default:
	}
}
