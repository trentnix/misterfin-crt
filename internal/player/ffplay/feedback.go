package ffplay

import (
	"io"
	"math"
	"strconv"
	"strings"

	"misterfin-crt/internal/player"
	"misterfin-crt/internal/player/feedback"
)

// Feedback creates an independent stdout/stderr parser for one process.
// emit receives validated observations and must return without blocking.
func (d Decoder) Feedback(emit func(player.Feedback)) io.Writer {
	return feedback.NewWriter(parseFeedback, emit)
}

// parseFeedback recognizes FFplay's continuously rewritten clock status. Startup
// logs and nonfinite, negative, or implausibly large timestamps are ignored.
func parseFeedback(line string) (player.Feedback, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 || (fields[1] != "A-V:" && fields[1] != "M-V:" && fields[1] != "M-A:") {
		return player.Feedback{}, false
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds >= 1e9 {
		return player.Feedback{}, false
	}
	return player.Feedback{Kind: player.FeedbackPosition, Position: seconds}, true
}
