package pythonhelper

import (
	"io"

	"misterfin-crt/internal/player"
	"misterfin-crt/internal/player/feedback"
)

// Feedback creates an independent stdout/stderr parser for one process.
// emit receives validated observations and must return without blocking.
func (d Decoder) Feedback(emit func(player.Feedback)) io.Writer {
	return feedback.NewWriter(feedback.ParseANS, emit)
}
