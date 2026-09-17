// Package feedback shares bounded line framing and the ANS status protocol used
// by patched MPlayer and the Python helper. FFplay supplies its own line parser.
package feedback

import (
	"io"
	"strings"
	"sync"

	"mistervision/internal/player"
)

// writer serializes stdout and stderr fragments. Complete lines are discarded
// after parsing.
type writer struct {
	mu      sync.Mutex
	pending []byte
	parse   func(string) (player.Feedback, bool)
	emit    func(player.Feedback)
}

// NewWriter frames CR/LF-delimited output and invokes parse for each line. It
// retains at most 8192 bytes of an unfinished line. Unknown output is discarded.
// The returned writer accepts concurrent writes. It serializes parse and emit.
// emit must not block on playback or call back into this writer.
func NewWriter(parse func(string) (player.Feedback, bool), emit func(player.Feedback)) io.Writer {
	return &writer{parse: parse, emit: emit}
}

// Write consumes all bytes, retaining partial lines across writes. Parsing and
// delivery finish before the call returns. No raw output is logged or returned.
func (w *writer) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, b := range data {
		if b == '\r' || b == '\n' {
			line := strings.TrimSpace(string(w.pending))
			w.pending = w.pending[:0]
			if value, ok := w.parse(line); ok {
				w.emit(value)
			}
		} else if len(w.pending) < 8192 {
			w.pending = append(w.pending, b)
		}
	}
	return len(data), nil
}
